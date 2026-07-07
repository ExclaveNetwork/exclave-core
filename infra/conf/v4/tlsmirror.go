package v4

import (
	"encoding/base64"
	"strings"

	"github.com/golang/protobuf/proto"

	"github.com/exclavenetwork/exclave-core/v5/common/serial"
	"github.com/exclavenetwork/exclave-core/v5/infra/conf/cfgcommon/tlscfg"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet/tlsmirror/mirrorenrollment"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet/tlsmirror/server"
	"github.com/exclavenetwork/exclave-core/v5/transport/internet/tlsmirror/tlstrafficgen"
)

type TLSMirrorConfig struct {
	ForwardAddress                string                                   `json:"forwardAddress"`
	ForwardPort                   uint32                                   `json:"forwardPort"`
	ForwardTag                    string                                   `json:"dorwardTag"`
	CarrierConnectionTag          string                                   `json:"carrierConnectionTag"`
	EmbeddedTrafficGenerator      *TLSMirrorEmbeddedTrafficGeneratorConfig `json:"embeddedTrafficGenerator"`
	PrimaryKey                    string                                   `json:"primaryKey"`
	ExplicitNonceCiphersuites     []uint32                                 `json:"explicitNonceCiphersuites"`
	DeferInstanceDerivedWriteTime *TLSMirrorTimeSpecConfig                 `json:"deferInstanceDerivedWriteTime"`
	TransportLayerPadding         *TLSMirrorTransportPaddingConfig         `json:"transportLayerPadding"`
	ConnectionEnrolment           *TLSMirrorConnectionEnrolmentConfig      `json:"connectionEnrolment"`
	SequenceWatermarkingEnabled   bool                                     `json:"sequenceWatermarkingEnabled"`
}

type TLSMirrorEmbeddedTrafficGeneratorConfig struct {
	Steps []*TLSMirrorEmbeddedTrafficGeneratorStepConfig `json:"steps"`
	// SecuritySettings
	TLSSettings  *tlscfg.TLSConfig  `json:"tlsSettings"`
	UTLSSettings *tlscfg.UTLSConfig `json:"utlsSettings"`
}

type TLSMirrorEmbeddedTrafficGeneratorStepConfig struct {
	Name                         string                                                      `json:"name"`
	Host                         string                                                      `json:"host"`
	Path                         string                                                      `json:"path"`
	Method                       string                                                      `json:"method"`
	NextStep                     []*TLSMirrorEmbeddedTrafficGeneratorTransferCandidateConfig `json:"nextStep"`
	ConnectionReady              bool                                                        `json:"connectionReady"`
	Headers                      []*TLSMirrorEmbeddedTrafficGeneratorHeaderConfig            `json:"headers"`
	ConnectionRecallExit         bool                                                        `json:"connectionRecallExit"`
	WaitTime                     *TLSMirrorEmbeddedTrafficGeneratorTimeSpecConfig            `json:"waitTime"`
	H2DoNotWaitForDownloadFinish bool                                                        `json:"h2DoNotWaitForDownloadFinish"`
}

type TLSMirrorEmbeddedTrafficGeneratorTimeSpecConfig struct {
	BaseNanoseconds                    uint64 `json:"baseNanoseconds"`
	UniformRandomMultiplierNanoseconds uint64 `json:"uniformRandomMultiplierNanoseconds"`
}

type TLSMirrorEmbeddedTrafficGeneratorTransferCandidateConfig struct {
	Weight       int32 `json:"weight"`
	GotoLocation int64 `json:"gotoLocation"`
}

type TLSMirrorEmbeddedTrafficGeneratorHeaderConfig struct {
	Name   string   `json:"name"`
	Value  string   `json:"value"`
	Values []string `json:"values"`
}

type TLSMirrorConnectionEnrolmentConfig struct {
	PrimaryIngressOutbound  string `json:"primaryIngressOutbound"`
	PrimaryEgressOutbound   string `json:"primaryEgressOutbound"`
	BootstrapEgressOutbound string `json:"bootstrapEgressOutbound"`
	// BootstrapIngressUrl
	// BootstrapEgressUrl
	// BootstrapIngressConfig
	// BootstrapEgressConfig
}

type TLSMirrorTimeSpecConfig struct {
	BaseNanoseconds                    uint64 `json:"baseNanoseconds"`
	UniformRandomMultiplierNanoseconds uint64 `json:"uniformRandomMultiplierNanoseconds"`
}

type TLSMirrorTransportPaddingConfig struct {
	Enabled bool `json:"enabled"`
}

func (c *TLSMirrorConfig) Build() (proto.Message, error) {
	config := &server.Config{
		ForwardAddress:              c.ForwardAddress,
		ForwardPort:                 c.ForwardPort,
		ForwardTag:                  c.ForwardTag,
		CarrierConnectionTag:        c.CarrierConnectionTag,
		ExplicitNonceCiphersuites:   c.ExplicitNonceCiphersuites,
		SequenceWatermarkingEnabled: c.SequenceWatermarkingEnabled,
	}
	if len(c.PrimaryKey) > 0 {
		primaryKey, err := base64.StdEncoding.DecodeString(c.PrimaryKey)
		if err != nil {
			return nil, err
		}
		config.PrimaryKey = primaryKey
	}
	if c.DeferInstanceDerivedWriteTime != nil {
		config.DeferInstanceDerivedWriteTime = &server.TimeSpec{
			BaseNanoseconds:                    c.DeferInstanceDerivedWriteTime.BaseNanoseconds,
			UniformRandomMultiplierNanoseconds: c.DeferInstanceDerivedWriteTime.UniformRandomMultiplierNanoseconds,
		}
	}
	if c.TransportLayerPadding != nil {
		config.TransportLayerPadding = &server.TransportLayerPadding{
			Enabled: c.TransportLayerPadding.Enabled,
		}
	}
	if c.EmbeddedTrafficGenerator != nil {
		config.EmbeddedTrafficGenerator = &tlstrafficgen.Config{
			Steps: make([]*tlstrafficgen.Step, len(config.EmbeddedTrafficGenerator.Steps)),
		}
		for i, step := range c.EmbeddedTrafficGenerator.Steps {
			s := &tlstrafficgen.Step{
				Name:                         step.Name,
				Host:                         step.Host,
				Path:                         step.Path,
				Method:                       step.Method,
				ConnectionReady:              step.ConnectionReady,
				ConnectionRecallExit:         step.ConnectionRecallExit,
				H2DoNotWaitForDownloadFinish: step.H2DoNotWaitForDownloadFinish,
			}
			if step.NextStep != nil {
				s.NextStep = make([]*tlstrafficgen.TransferCandidate, len(step.NextStep))
				for i, nextStep := range step.NextStep {
					s.NextStep[i] = &tlstrafficgen.TransferCandidate{
						Weight:       nextStep.Weight,
						GotoLocation: nextStep.GotoLocation,
					}
				}
			}
			if step.Headers != nil {
				s.Headers = make([]*tlstrafficgen.Header, len(step.Headers))
				for i, header := range step.Headers {
					s.Headers[i] = &tlstrafficgen.Header{
						Name:   header.Name,
						Value:  header.Value,
						Values: header.Values,
					}
				}
			}
			if step.WaitTime != nil {
				s.WaitTime = &tlstrafficgen.TimeSpec{
					BaseNanoseconds:                    step.WaitTime.BaseNanoseconds,
					UniformRandomMultiplierNanoseconds: step.WaitTime.UniformRandomMultiplierNanoseconds,
				}
			}
			config.EmbeddedTrafficGenerator.Steps[i] = s
		}
		if c.EmbeddedTrafficGenerator.TLSSettings != nil {
			if c.EmbeddedTrafficGenerator.TLSSettings.Fingerprint != "" {
				imitate := strings.ToLower(c.EmbeddedTrafficGenerator.TLSSettings.Fingerprint)
				imitate = strings.TrimPrefix(imitate, "hello")
				switch imitate {
				case "chrome", "firefox", "safari", "ios", "edge", "360", "qq":
					imitate += "_auto"
				}
				utlsConfig := &tlscfg.UTLSConfig{
					TLSConfig: c.EmbeddedTrafficGenerator.TLSSettings,
					Imitate:   imitate,
				}
				utlsSettings, err := utlsConfig.Build()
				if err != nil {
					return nil, err
				}
				config.EmbeddedTrafficGenerator.SecuritySettings = serial.ToTypedMessage(utlsSettings)
			} else {
				tlsSettings, err := c.EmbeddedTrafficGenerator.TLSSettings.Build()
				if err != nil {
					return nil, err
				}
				config.EmbeddedTrafficGenerator.SecuritySettings = serial.ToTypedMessage(tlsSettings)
			}
		}
		if c.EmbeddedTrafficGenerator.UTLSSettings != nil {
			utlsSettings, err := c.EmbeddedTrafficGenerator.UTLSSettings.Build()
			if err != nil {
				return nil, err
			}
			config.EmbeddedTrafficGenerator.SecuritySettings = serial.ToTypedMessage(utlsSettings)
		}
	}
	if c.ConnectionEnrolment != nil {
		config.ConnectionEnrolment = &mirrorenrollment.Config{
			PrimaryIngressOutbound:  c.ConnectionEnrolment.PrimaryIngressOutbound,
			PrimaryEgressOutbound:   c.ConnectionEnrolment.PrimaryEgressOutbound,
			BootstrapEgressOutbound: c.ConnectionEnrolment.BootstrapEgressOutbound,
			// BootstrapIngressUrl
			// BootstrapEgressUrl
			// BootstrapIngressConfig
			// BootstrapEgressConfig
		}
	}
	return config, nil
}
