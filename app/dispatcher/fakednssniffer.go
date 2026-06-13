package dispatcher

import (
	"context"

	core "github.com/exclavenetwork/exclave-core/v5"
	"github.com/exclavenetwork/exclave-core/v5/common"
	"github.com/exclavenetwork/exclave-core/v5/common/net"
	"github.com/exclavenetwork/exclave-core/v5/common/session"
	"github.com/exclavenetwork/exclave-core/v5/features/dns"
)

// newFakeDNSSniffer Creates a Fake DNS metadata sniffer
func newFakeDNSSniffer(ctx context.Context) (protocolSnifferWithMetadata, error) {
	var fakeDNSEngine dns.FakeDNSEngine
	{
		fakeDNSEngineFeat := core.MustFromContext(ctx).GetFeature((*dns.FakeDNSEngine)(nil))
		if fakeDNSEngineFeat != nil {
			fakeDNSEngine = fakeDNSEngineFeat.(dns.FakeDNSEngine)
		}
	}

	if fakeDNSEngine == nil {
		errNotInit := newError("FakeDNSEngine is not initialized, but such a sniffer is used").AtError()
		return protocolSnifferWithMetadata{}, errNotInit
	}
	return protocolSnifferWithMetadata{protocolSniffer: func(ctx context.Context, bytes []byte) (SniffResult, error) {
		Target := session.OutboundFromContext(ctx).Target
		if Target.Network == net.Network_TCP || Target.Network == net.Network_UDP {
			domainFromFakeDNS := fakeDNSEngine.GetDomainFromFakeDNS(Target.Address)
			if domainFromFakeDNS != "" {
				newError("fake dns got domain: ", domainFromFakeDNS, " for ip: ", Target.Address.String()).WriteToLog(session.ExportIDToError(ctx))
				return &fakeDNSSniffResult{domainName: domainFromFakeDNS}, nil
			}
		}

		if ipAddressInRangeValueI := ctx.Value(ipAddressInRange); ipAddressInRangeValueI != nil {
			ipAddressInRangeValue := ipAddressInRangeValueI.(*ipAddressInRangeOpt)
			if fkr0, ok := fakeDNSEngine.(dns.FakeDNSEngineRev0); ok {
				inPool := fkr0.IsIPInIPPool(Target.Address)
				ipAddressInRangeValue.addressInRange = &inPool
			}
		}

		return nil, common.ErrNoClue
	}, metadataSniffer: true}, nil
}

type fakeDNSSniffResult struct {
	domainName string
}

func (fakeDNSSniffResult) Protocol() string {
	return "fakedns"
}

func (f fakeDNSSniffResult) Domain() string {
	return f.domainName
}

type fakeDNSExtraOpts int

const ipAddressInRange fakeDNSExtraOpts = 1

type ipAddressInRangeOpt struct {
	addressInRange *bool
}
