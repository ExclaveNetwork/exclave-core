package snellv5

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"io"
	"math/big"
	"math/bits"
	"sync"
	"time"

	snell "github.com/exclavenetwork/exclave-core/v5/proxy/snell/internal/singsnell"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
	N "github.com/sagernet/sing/common/network"
)

const (
	// snell-server v5.0.1: FUN_0013bbe0 calls getsockopt(TCP_MAXSEG) on the
	// accepted client socket but, on the normal success path, passes 0x5b4 to
	// FUN_00139080; FUN_00139670 then computes the first payload limit as
	// frameSize - 0x37 - paddingLen, and uses frameSize - 0x27 for growth.
	maxPayload                = snell.MaxPayloadLen
	frameSize                 = 0x5b4
	firstRecordOverhead       = 0x37
	resetRecordOverhead       = 0x27
	initialPaddingMin         = 0x100
	initialPaddingSpan        = 0x100
	maxInitialPaddingLen      = initialPaddingMin + initialPaddingSpan - 1
	framePayloadStep          = frameSize - resetRecordOverhead
	framePayloadResetInterval = 31
)

type reader struct {
	upstream        io.Reader
	cipher          cipher.AEAD
	nonce           []byte
	cache           *buf.Buffer
	readWaitOptions N.ReadWaitOptions
}

func newReader(upstream io.Reader, aead cipher.AEAD, nonce []byte) *reader {
	return &reader{upstream: upstream, cipher: aead, nonce: nonce}
}

func (r *reader) ReadRecord() (*buf.Buffer, error) {
	headerCipher := buf.NewSize(snell.HeaderCipherLen)
	_, err := headerCipher.ReadFullFrom(r.upstream, snell.HeaderCipherLen)
	if err != nil {
		headerCipher.Release()
		return nil, err
	}
	_, err = r.cipher.Open(headerCipher.Index(0), r.nonce, headerCipher.Bytes(), nil)
	if err != nil {
		headerCipher.Release()
		return nil, E.Cause(err, "open record header")
	}
	snell.IncreaseNonce(r.nonce)
	header := headerCipher.To(snell.HeaderPlainLen)
	if header[0] != snell.HeaderVersion {
		headerCipher.Release()
		return nil, E.Extend(snell.ErrBadVersion, header[0])
	}
	paddingLen := int(binary.BigEndian.Uint16(header[3:5]))
	payloadLen := int(binary.BigEndian.Uint16(header[5:7]))
	headerCipher.Release()

	if payloadLen == 0 {
		return nil, io.EOF
	}

	var padding *buf.Buffer
	if paddingLen > 0 {
		padding = buf.NewSize(paddingLen)
		_, err = padding.ReadFullFrom(r.upstream, paddingLen)
		if err != nil {
			padding.Release()
			return nil, err
		}
	}
	body := r.readWaitOptions.NewBufferSize(payloadLen + snell.AEADTagLen)
	_, err = body.ReadFullFrom(r.upstream, payloadLen+snell.AEADTagLen)
	if err != nil {
		if padding != nil {
			padding.Release()
		}
		body.Release()
		return nil, err
	}
	if padding != nil {
		paddingBytes := padding.Bytes()
		payloadCipher := body.Bytes()
		limit := min(len(payloadCipher), len(paddingBytes))
		for index := 0; index < limit; index += 2 {
			paddingBytes[index], payloadCipher[index] = payloadCipher[index], paddingBytes[index]
		}
		padding.Release()
	}
	payloadCipher := body.Bytes()
	_, err = r.cipher.Open(payloadCipher[:0], r.nonce, payloadCipher, nil)
	if err != nil {
		body.Release()
		return nil, E.Cause(err, "open record payload")
	}
	snell.IncreaseNonce(r.nonce)
	body.Truncate(payloadLen)
	r.readWaitOptions.PostReturn(body)
	return body, nil
}

func (r *reader) NextRecord() (*buf.Buffer, error) {
	record := r.cache
	if record != nil {
		r.cache = nil
		if record.IsEmpty() {
			record.Release()
		} else {
			return record, nil
		}
	}
	return r.ReadRecord()
}

func (r *reader) SetCache(cache *buf.Buffer) {
	r.cache = cache
}

func (r *reader) ReleaseCache() {
	if r.cache != nil {
		r.cache.Release()
		r.cache = nil
	}
}

func (r *reader) Read(p []byte) (n int, err error) {
	for {
		if r.cache != nil {
			if r.cache.IsEmpty() {
				r.cache.Release()
				r.cache = nil
			} else {
				n = copy(p, r.cache.Bytes())
				r.cache.Advance(n)
				return
			}
		}
		r.cache, err = r.ReadRecord()
		if err != nil {
			return
		}
	}
}

func (r *reader) ReadBuffer(buffer *buf.Buffer) error {
	for {
		if r.cache != nil {
			if r.cache.IsEmpty() {
				r.cache.Release()
				r.cache = nil
			} else {
				n, _ := buffer.Write(r.cache.Bytes())
				r.cache.Advance(n)
				return nil
			}
		}
		var err error
		r.cache, err = r.ReadRecord()
		if err != nil {
			return err
		}
	}
}

func (r *reader) InitializeReadWaiter(options N.ReadWaitOptions) (needCopy bool) {
	r.readWaitOptions = options
	return false
}

func (r *reader) WaitReadBuffer() (buffer *buf.Buffer, err error) {
	for {
		if r.cache == nil {
			return r.ReadRecord()
		}
		buffer = r.cache
		r.cache = nil
		if buffer.IsEmpty() {
			buffer.Release()
			continue
		}
		if buffer.Start() >= r.readWaitOptions.FrontHeadroom && buffer.FreeLen() >= r.readWaitOptions.RearHeadroom {
			return
		}
		buffer = r.readWaitOptions.Copy(buffer)
		return buffer, nil
	}
}

func (r *reader) Upstream() any {
	return r.upstream
}

type writer struct {
	upstream          io.Writer
	cipher            cipher.AEAD
	nonce             []byte
	access            sync.Mutex
	lastFrameUnix     int64
	framePayloadLen   int
	initialPaddingLen int
}

func newWriter(upstream io.Writer, aead cipher.AEAD, nonce []byte) (*writer, error) {
	paddingDelta, err := rand.Int(rand.Reader, big.NewInt(initialPaddingSpan))
	if err != nil {
		return nil, err
	}
	return &writer{
		upstream:          upstream,
		cipher:            aead,
		nonce:             nonce,
		initialPaddingLen: initialPaddingMin + int(paddingDelta.Int64()),
	}, nil
}

func (w *writer) sealHeader(header []byte, paddingLen int, payloadLen int) {
	header[0] = snell.HeaderVersion
	header[1] = 0
	header[2] = 0
	binary.BigEndian.PutUint16(header[3:5], uint16(paddingLen))
	binary.BigEndian.PutUint16(header[5:7], uint16(payloadLen))
	w.cipher.Seal(header[:0], w.nonce, header[:snell.HeaderPlainLen], nil)
	snell.IncreaseNonce(w.nonce)
}

func (w *writer) nextPayloadLimit() int {
	now := time.Now().Unix()
	var payloadLimit int
	if w.lastFrameUnix == 0 {
		payloadLimit = frameSize - firstRecordOverhead - w.initialPaddingLen
	} else if now-w.lastFrameUnix < framePayloadResetInterval {
		payloadLimit = w.framePayloadLen
		if payloadLimit == 0 {
			payloadLimit = framePayloadStep
		}
	} else {
		payloadLimit = framePayloadStep
	}
	w.lastFrameUnix = now
	w.markPayloadLimitUsed(payloadLimit)
	return payloadLimit
}

func (w *writer) markPayloadLimitUsed(payloadLimit int) {
	nextPayloadLimit := min(payloadLimit+framePayloadStep, maxPayload)
	w.framePayloadLen = nextPayloadLimit
	if w.lastFrameUnix == 0 {
		w.lastFrameUnix = time.Now().Unix()
	}
}

func (w *writer) randomInt31() (int, error) {
	var randomBytes [4]byte
	_, err := io.ReadFull(rand.Reader, randomBytes[:])
	if err != nil {
		return 0, err
	}
	return int(binary.LittleEndian.Uint32(randomBytes[:]) & 0x7fffffff), nil
}

func (w *writer) Write(p []byte) (n int, err error) {
	w.access.Lock()
	defer w.access.Unlock()
	payloadLimit := w.nextPayloadLimit()
	if len(p) == 0 {
		return 0, nil
	}
	// snell-server v5.0.1: FUN_00139670 encrypts every record of one encrypt call
	// into a single malloc'd output buffer that the caller writes to the socket once.
	recordCount := (len(p) + payloadLimit - 1) / payloadLimit
	output := buf.NewSize(recordCount*(snell.HeaderCipherLen+snell.AEADTagLen) + len(p))
	defer output.Release()
	for data := p; len(data) > 0; {
		recordLen := min(len(data), payloadLimit)
		err = w.sealRecordLocked(output, data[:recordLen], 0)
		if err != nil {
			return 0, err
		}
		data = data[recordLen:]
	}
	_, err = w.upstream.Write(output.Bytes())
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *writer) WriteFirst(salt []byte, payload []byte) error {
	w.access.Lock()
	defer w.access.Unlock()
	// snell-server v5.0.1: FUN_0013f020 prefixes the response byte and then calls
	// FUN_00139670 once; that call reuses one payload limit for every emitted chunk
	// and returns the salt plus all records in one buffer written to the socket once.
	payloadLimit := w.nextPayloadLimit()
	if len(payload) == 0 {
		record := buf.NewSize(len(salt) + snell.HeaderCipherLen)
		common.Must1(record.Write(salt))
		w.sealHeader(record.Extend(snell.HeaderCipherLen), 0, 0)
		err := common.Error(w.upstream.Write(record.Bytes()))
		record.Release()
		return err
	}
	recordCount := (len(payload) + payloadLimit - 1) / payloadLimit
	output := buf.NewSize(len(salt) + w.initialPaddingLen + recordCount*(snell.HeaderCipherLen+snell.AEADTagLen) + len(payload))
	defer output.Release()
	common.Must1(output.Write(salt))
	paddingLen := w.initialPaddingLen
	for data := payload; len(data) > 0; {
		recordLen := min(len(data), payloadLimit)
		err := w.sealRecordLocked(output, data[:recordLen], paddingLen)
		if err != nil {
			return err
		}
		paddingLen = 0
		data = data[recordLen:]
	}
	return common.Error(w.upstream.Write(output.Bytes()))
}

func (w *writer) WriteFirstVectorised(salt []byte, buffers []*buf.Buffer, upstream N.VectorisedWriter) error {
	payloadLen := buf.LenMulti(buffers)
	var records []*buf.Buffer
	defer func() {
		buf.ReleaseMulti(records)
	}()
	w.access.Lock()
	defer w.access.Unlock()
	payloadLimit := w.nextPayloadLimit()
	if payloadLen == 0 {
		buf.ReleaseMulti(buffers)
		record := buf.NewSize(len(salt) + snell.HeaderCipherLen)
		common.Must1(record.Write(salt))
		w.sealHeader(record.Extend(snell.HeaderCipherLen), 0, 0)
		records = append(records, record)
		flushRecords := records
		records = nil
		return upstream.WriteVectorised(flushRecords)
	}
	saltPrefix := salt
	paddingLen := w.initialPaddingLen
	index := 0
	// snell-server v5.0.1: FUN_00139670 advances the growth window once per
	// encrypt call and splits that call using the same payload limit.
	for remainingPayload := payloadLen; remainingPayload > 0; {
		for buffers[index].IsEmpty() {
			buffers[index].Release()
			index++
		}
		recordLen := min(remainingPayload, payloadLimit)
		buffer := buffers[index]
		if len(saltPrefix) == 0 && paddingLen == 0 && buffer.Len() == recordLen {
			record, err := w.makeBufferRecordLocked(buffer, 0)
			if err != nil {
				buffer.Release()
				buf.ReleaseMulti(buffers[index+1:])
				return err
			}
			records = append(records, record)
			index++
		} else {
			payload := make([]byte, recordLen)
			for copiedLen := 0; copiedLen < recordLen; {
				buffer = buffers[index]
				if buffer.IsEmpty() {
					buffer.Release()
					index++
					continue
				}
				copyLen := min(recordLen-copiedLen, buffer.Len())
				copy(payload[copiedLen:], buffer.Bytes()[:copyLen])
				buffer.Advance(copyLen)
				copiedLen += copyLen
				if buffer.IsEmpty() {
					buffer.Release()
					index++
				}
			}
			record := buf.NewSize(len(saltPrefix) + snell.HeaderCipherLen + paddingLen + recordLen + snell.AEADTagLen)
			common.Must1(record.Write(saltPrefix))
			err := w.sealRecordLocked(record, payload, paddingLen)
			if err != nil {
				record.Release()
				buf.ReleaseMulti(buffers[index:])
				return err
			}
			records = append(records, record)
		}
		remainingPayload -= recordLen
		if len(saltPrefix) > 0 {
			saltPrefix = nil
		}
		paddingLen = 0
	}
	buf.ReleaseMulti(buffers[index:])
	flushRecords := records
	records = nil
	return upstream.WriteVectorised(flushRecords)
}

func (w *writer) sealRecordLocked(output *buf.Buffer, payload []byte, paddingLen int) error {
	if len(payload) > maxPayload || paddingLen > maxPayload {
		panic("snell: v5 record exceeds maximum")
	}
	if len(payload) == 0 && paddingLen != 0 {
		panic("snell: zero-length v5 record carries padding")
	}
	w.sealHeader(output.Extend(snell.HeaderCipherLen), paddingLen, len(payload))
	padding := output.Extend(paddingLen)
	payloadCipher := output.Extend(len(payload) + snell.AEADTagLen)
	copy(payloadCipher, payload)
	w.cipher.Seal(payloadCipher[:0], w.nonce, payloadCipher[:len(payload)], nil)
	snell.IncreaseNonce(w.nonce)
	if paddingLen > 0 {
		err := w.fillPadding(padding, payloadCipher)
		if err != nil {
			return err
		}
		limit := min(len(padding), len(payloadCipher))
		for index := 0; index < limit; index += 2 {
			padding[index], payloadCipher[index] = payloadCipher[index], padding[index]
		}
	}
	return nil
}

func (w *writer) makeBufferRecordLocked(buffer *buf.Buffer, paddingLen int) (*buf.Buffer, error) {
	dataLen := buffer.Len()
	if dataLen > maxPayload || paddingLen > maxPayload {
		panic("snell: v5 record exceeds maximum")
	}
	if dataLen == 0 && paddingLen != 0 {
		panic("snell: zero-length v5 record carries padding")
	}
	prefix := buffer.ExtendHeader(snell.HeaderCipherLen + paddingLen)
	header := prefix[:snell.HeaderCipherLen]
	w.sealHeader(header, paddingLen, dataLen)
	payloadCipher := buffer.From(snell.HeaderCipherLen + paddingLen)
	buffer.Extend(snell.AEADTagLen)
	w.cipher.Seal(payloadCipher[:0], w.nonce, payloadCipher, nil)
	payloadCipher = buffer.From(snell.HeaderCipherLen + paddingLen)
	snell.IncreaseNonce(w.nonce)
	if paddingLen > 0 {
		padding := prefix[snell.HeaderCipherLen:]
		err := w.fillPadding(padding, payloadCipher)
		if err != nil {
			return nil, err
		}
		limit := min(len(padding), len(payloadCipher))
		for index := 0; index < limit; index += 2 {
			padding[index], payloadCipher[index] = payloadCipher[index], padding[index]
		}
	}
	return buffer, nil
}

func (w *writer) WriteBuffer(buffer *buf.Buffer) error {
	defer buffer.Release()
	dataLen := buffer.Len()
	if dataLen == 0 {
		return nil
	}
	w.access.Lock()
	defer w.access.Unlock()
	payloadLimit := w.nextPayloadLimit()
	if dataLen > payloadLimit {
		recordCount := (dataLen + payloadLimit - 1) / payloadLimit
		output := buf.NewSize(recordCount*(snell.HeaderCipherLen+snell.AEADTagLen) + dataLen)
		defer output.Release()
		for data := buffer.Bytes(); len(data) > 0; {
			recordLen := min(len(data), payloadLimit)
			err := w.sealRecordLocked(output, data[:recordLen], 0)
			if err != nil {
				return err
			}
			data = data[recordLen:]
		}
		return common.Error(w.upstream.Write(output.Bytes()))
	}
	record, err := w.makeBufferRecordLocked(buffer, 0)
	if err != nil {
		return err
	}
	defer record.Release()
	return common.Error(w.upstream.Write(record.Bytes()))
}

func (w *writer) WritePacketBuffer(buffer *buf.Buffer) error {
	dataLen := buffer.Len()
	w.access.Lock()
	defer w.access.Unlock()
	now := time.Now().Unix()
	var payloadLimit int
	if w.lastFrameUnix == 0 {
		payloadLimit = frameSize - firstRecordOverhead - w.initialPaddingLen
	} else if now-w.lastFrameUnix < framePayloadResetInterval {
		payloadLimit = w.framePayloadLen
		if payloadLimit == 0 {
			payloadLimit = framePayloadStep
		}
	} else {
		payloadLimit = framePayloadStep
	}
	if dataLen > payloadLimit {
		buffer.Release()
		return snell.ErrPayloadTooLarge
	}
	w.lastFrameUnix = now
	w.markPayloadLimitUsed(payloadLimit)
	record, err := w.makeBufferRecordLocked(buffer, 0)
	if err != nil {
		buffer.Release()
		return err
	}
	defer record.Release()
	return common.Error(w.upstream.Write(record.Bytes()))
}

func (w *writer) WriteZeroChunk() error {
	buffer := buf.NewSize(snell.HeaderCipherLen)
	w.access.Lock()
	defer w.access.Unlock()
	// snell-server v5.0.1: FUN_0013def0 calls FUN_00139670 even for an empty
	// payload, so EOF records consume one growth-window step.
	w.nextPayloadLimit()
	w.sealHeader(buffer.Extend(snell.HeaderCipherLen), 0, 0)
	err := common.Error(w.upstream.Write(buffer.Bytes()))
	buffer.Release()
	return err
}

func (w *writer) fillPadding(padding []byte, payloadCipher []byte) error {
	if len(padding) == 0 {
		return nil
	}
	oneBits := 0
	// snell-server v5.0.1: FUN_00138af0 counts ones only over complete
	// 32-bit chunks, but computes zeros against the full payload cipher length.
	payloadCountLen := len(payloadCipher) &^ 3
	for _, payloadByte := range payloadCipher[:payloadCountLen] {
		oneBits += bits.OnesCount8(payloadByte)
	}
	zeroBits := 8*len(payloadCipher) - oneBits
	if zeroBits <= 0 {
		_, err := io.ReadFull(rand.Reader, padding)
		return err
	}

	ratio := float64(oneBits) / float64(zeroBits)
	if ratio <= 0.5 || ratio >= 1.6 {
		_, err := io.ReadFull(rand.Reader, padding)
		return err
	}

	targetRatioBase := 1.6
	if zeroBits < oneBits {
		targetRatioBase = 0.4
	}
	// snell-server v5.0.1: FUN_00138af0 derives the target padding ratio as
	// base + rand()/2147483647.0/10, using the process rand() 31-bit range.
	randomValue, err := w.randomInt31()
	if err != nil {
		return err
	}
	targetRatio := targetRatioBase + float64(randomValue)/2147483647.0/10
	targetOnes := int(float64(8*(len(payloadCipher)+len(padding)))*targetRatio/(targetRatio+1) - float64(oneBits))
	if targetOnes < 0 || targetOnes > 8*len(padding) {
		_, err = io.ReadFull(rand.Reader, padding)
		return err
	}
	return w.fillPaddingWithBitCount(padding, targetOnes)
}

func (w *writer) fillPaddingWithBitCount(padding []byte, oneBits int) error {
	totalBits := 8 * len(padding)
	if oneBits < 0 || oneBits > totalBits {
		panic("snell: invalid padding bit count")
	}
	bitset := make([]byte, totalBits)
	for index := range oneBits {
		bitset[index] = 1
	}
	// snell-server v5.0.1: FUN_00138af0 shuffles from the front and maps rand()
	// with division by RAND_MAX/remaining+1, preserving its modulo bias.
	position := 0
	remaining := totalBits
	for remaining != 1 {
		randomValue, err := w.randomInt31()
		if err != nil {
			return err
		}
		divisor := 0
		if remaining != 0 {
			divisor = 0x7fffffff / remaining
		}
		swapIndex := position + randomValue/(divisor+1)
		bitset[swapIndex], bitset[position] = bitset[position], bitset[swapIndex]
		position++
		remaining--
	}
	clear(padding)
	for index, bit := range bitset {
		if bit == 1 {
			padding[index/8] |= 1 << uint(index%8)
		}
	}
	return nil
}

func (w *writer) CreateVectorisedWriter() (N.VectorisedWriter, bool) {
	upstreamWriter, created := bufio.CreateVectorisedWriter(w.upstream)
	if !created {
		return nil, false
	}
	return w.CreateVectorisedWriterFor(upstreamWriter), true
}

func (w *writer) CreateVectorisedWriterFor(upstream N.VectorisedWriter) N.VectorisedWriter {
	return &vectorisedWriter{writer: w, upstream: upstream}
}

type vectorisedWriter struct {
	writer   *writer
	upstream N.VectorisedWriter
}

func (w *vectorisedWriter) WriteVectorised(buffers []*buf.Buffer) error {
	payloadLen := buf.LenMulti(buffers)
	if payloadLen == 0 {
		buf.ReleaseMulti(buffers)
		return nil
	}
	var records []*buf.Buffer
	defer func() {
		buf.ReleaseMulti(records)
	}()
	recordWriter := w.writer
	recordWriter.access.Lock()
	defer recordWriter.access.Unlock()
	// snell-server v5.0.1: FUN_00139670 advances the growth window once per
	// encrypt call and splits that call using the same payload limit.
	payloadLimit := recordWriter.nextPayloadLimit()
	index := 0
	for remainingPayload := payloadLen; remainingPayload > 0; {
		for buffers[index].IsEmpty() {
			buffers[index].Release()
			index++
		}
		recordLen := min(remainingPayload, payloadLimit)
		buffer := buffers[index]
		if buffer.Len() == recordLen {
			record, err := recordWriter.makeBufferRecordLocked(buffer, 0)
			if err != nil {
				buffer.Release()
				buf.ReleaseMulti(buffers[index+1:])
				return err
			}
			records = append(records, record)
			index++
		} else {
			payload := make([]byte, recordLen)
			for copiedLen := 0; copiedLen < recordLen; {
				buffer = buffers[index]
				if buffer.IsEmpty() {
					buffer.Release()
					index++
					continue
				}
				copyLen := min(recordLen-copiedLen, buffer.Len())
				copy(payload[copiedLen:], buffer.Bytes()[:copyLen])
				buffer.Advance(copyLen)
				copiedLen += copyLen
				if buffer.IsEmpty() {
					buffer.Release()
					index++
				}
			}
			record := buf.NewSize(snell.HeaderCipherLen + recordLen + snell.AEADTagLen)
			err := recordWriter.sealRecordLocked(record, payload, 0)
			if err != nil {
				record.Release()
				buf.ReleaseMulti(buffers[index:])
				return err
			}
			records = append(records, record)
		}
		remainingPayload -= recordLen
	}
	buf.ReleaseMulti(buffers[index:])
	flushRecords := records
	records = nil
	return w.upstream.WriteVectorised(flushRecords)
}

func (w *writer) CreatePacketVectorisedWriterFor(upstream N.VectorisedWriter) N.VectorisedWriter {
	return &packetVectorisedWriter{writer: w, upstream: upstream}
}

type packetVectorisedWriter struct {
	writer   *writer
	upstream N.VectorisedWriter
}

func (w *packetVectorisedWriter) WriteVectorised(buffers []*buf.Buffer) error {
	var records []*buf.Buffer
	defer func() {
		buf.ReleaseMulti(records)
	}()
	recordWriter := w.writer
	recordWriter.access.Lock()
	defer recordWriter.access.Unlock()
	for index, buffer := range buffers {
		dataLen := buffer.Len()
		if dataLen == 0 {
			buffer.Release()
			continue
		}
		now := time.Now().Unix()
		var payloadLimit int
		if recordWriter.lastFrameUnix == 0 {
			payloadLimit = frameSize - firstRecordOverhead - recordWriter.initialPaddingLen
		} else if now-recordWriter.lastFrameUnix < framePayloadResetInterval {
			payloadLimit = recordWriter.framePayloadLen
			if payloadLimit == 0 {
				payloadLimit = framePayloadStep
			}
		} else {
			payloadLimit = framePayloadStep
		}
		if dataLen > payloadLimit {
			buffer.Release()
			buf.ReleaseMulti(buffers[index+1:])
			return snell.ErrPayloadTooLarge
		}
		recordWriter.lastFrameUnix = now
		recordWriter.markPayloadLimitUsed(payloadLimit)
		record, err := recordWriter.makeBufferRecordLocked(buffer, 0)
		if err != nil {
			buffer.Release()
			buf.ReleaseMulti(buffers[index+1:])
			return err
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil
	}
	flushRecords := records
	records = nil
	return w.upstream.WriteVectorised(flushRecords)
}

func (w *writer) FrontHeadroom() int {
	return snell.HeaderCipherLen
}

func (w *writer) RearHeadroom() int {
	return snell.AEADTagLen
}

func (w *writer) WriterMTU() int {
	return maxPayload
}

func (w *writer) Upstream() any {
	return w.upstream
}

var (
	_ N.ExtendedReader         = (*reader)(nil)
	_ N.ReadWaiter             = (*reader)(nil)
	_ N.ExtendedWriter         = (*writer)(nil)
	_ N.VectorisedWriteCreator = (*writer)(nil)
	_ N.VectorisedWriter       = (*vectorisedWriter)(nil)
	_ N.VectorisedWriter       = (*packetVectorisedWriter)(nil)
	_ N.FrontHeadroom          = (*writer)(nil)
	_ N.RearHeadroom           = (*writer)(nil)
	_ N.WriterWithMTU          = (*writer)(nil)
)
