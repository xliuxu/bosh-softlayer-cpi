package common

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"sort"

	datatypes "github.com/maximilien/softlayer-go/data_types"
)

type Route struct {
	Network string `json:"network,omitempty"`
	Netmask string `json:"netmask,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

func SoftlayerPrivateRoutes(gateway string) []Route {
	return []Route{
		{Network: "10.0.0.0", Netmask: "255.0.0.0", Gateway: gateway},
		{Network: "161.26.0.0", Netmask: "255.255.0.0", Gateway: gateway},
	}
}

type Interface struct {
	Name                string
	Auto                bool
	AllowHotplug        bool
	DefaultGateway      bool
	SourcePolicyRouting bool
	Address             string
	Netmask             string
	Gateway             string
	Routes              []Route
	DNS                 []string
}

type Interfaces []Interface

const ETC_NETWORK_INTERFACES_TEMPLATE = `# Generated by softlayer-cpi
auto lo
iface lo inet loopback
{{ range . -}}
# {{ .Name }}
{{- if .Auto }}
auto {{ .Name }}
{{- end }}
{{- if .AllowHotplug }}
allow-hotplug {{ .Name }}
{{- end }}
iface {{ .Name }} inet static
    address {{ .Address }}
    netmask {{ .Netmask }}
    {{- if .DefaultGateway }}
    gateway {{ .Gateway }}
		{{- end }}
    {{- range $route := .Routes }}
    post-up route add -net {{ $route.Network }} netmask {{ $route.Netmask }} gw {{ $route.Gateway }}
    {{- end }}
{{- if .DNS }}
    dns-nameservers{{ range .DNS }} {{ . }}{{ end }}
{{- end }}
{{ end }}`

func (i Interfaces) Len() int           { return len(i) }
func (i Interfaces) Less(x, y int) bool { return i[x].Name < i[y].Name }
func (i Interfaces) Swap(x, y int)      { i[x], i[y] = i[y], i[x] }

func (i Interfaces) Configuration() ([]byte, error) {
	funcMap := template.FuncMap{
		"add": func(a, b int) string {
			return fmt.Sprintf("%d", a+b)
		},
	}
	t := template.Must(template.New("network-interfaces").Funcs(funcMap).Parse(ETC_NETWORK_INTERFACES_TEMPLATE))

	sort.Sort(i)
	buf := &bytes.Buffer{}
	err := t.Execute(buf, i)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

type Softlayer_Ubuntu_Net struct {
	LinkNamer            LinkNamer
}

func (u *Softlayer_Ubuntu_Net) NormalizeNetworkDefinitions(networks Networks, componentByNetwork map[string]datatypes.SoftLayer_Virtual_Guest_Network_Component) (Networks, error) {
	normalized := Networks{}

	for name, nw := range networks {
		switch nw.Type {
		case "dynamic":
			c := componentByNetwork[name]
			nw.IP = c.PrimaryIpAddress
			nw.MAC = c.MacAddress
			normalized[name] = nw
		case "manual", "":
			nw.Type = "manual"
			normalized[name] = nw
		default:
			return nil, fmt.Errorf("unexpected network type: %s", nw.Type)
		}
	}

	return normalized, nil
}

func (u *Softlayer_Ubuntu_Net) FinalizedNetworkDefinitions(networkComponents datatypes.SoftLayer_Virtual_Guest, networks Networks, componentByNetwork map[string]datatypes.SoftLayer_Virtual_Guest_Network_Component) (Networks, error) {
	finalized := Networks{}
	for name, nw := range networks {
		component, ok := componentByNetwork[name]
		if !ok {
			return networks, fmt.Errorf("network not found: %q", name)
		}

		subnet, err := component.NetworkVlan.Subnets.Containing(nw.IP)
		if err != nil {
			return networks,fmt.Errorf("Determining IP `%s`: `%s`",nw.IP, err.Error())
		}

		linkName := fmt.Sprintf("%s%d", component.Name, component.Port)
		if nw.Type != "dynamic" {
			linkName, err = u.LinkNamer.Name(linkName, name)
			if err != nil {
				return networks, fmt.Errorf("Linking network with name `%s`: `%s`", name, err.Error())
			}
		}

		nw.LinkName = linkName
		nw.Netmask = subnet.Netmask
		nw.Gateway = subnet.Gateway

		if component.NetworkVlan.Id == networkComponents.PrimaryBackendNetworkComponent.NetworkVlan.Id {
			nw.Routes = SoftlayerPrivateRoutes(subnet.Gateway)
		}

		finalized[name] = nw
	}

	return finalized, nil
}

func (u *Softlayer_Ubuntu_Net) NormalizeDynamics(networkComponents datatypes.SoftLayer_Virtual_Guest, networks Networks) (Networks, error) {
	var privateDynamic, publicDynamic *Network

	for _, nw := range networks {
		if nw.Type != "dynamic" {
			continue
		}

		if nw.CloudProperties.VlanID == networkComponents.PrimaryBackendNetworkComponent.NetworkVlan.Id {
			if privateDynamic != nil {
				return nil, errors.New("multiple private dynamic networks are not supported")
			}
			privateDynamic = &nw
		}

		if nw.CloudProperties.VlanID == networkComponents.PrimaryNetworkComponent.NetworkVlan.Id {
			if publicDynamic != nil {
				return nil, errors.New("multiple public dynamic networks are not supported")
			}
			publicDynamic = &nw
		}
	}

	if privateDynamic == nil {
		networks["generated-private"] = Network{
			Type:          "dynamic",
			Preconfigured: true,
			IP:            networkComponents.PrimaryBackendNetworkComponent.PrimaryIpAddress,
			CloudProperties: NetworkCloudProperties{
				VlanID:              networkComponents.PrimaryBackendNetworkComponent.NetworkVlan.Id,
				SourcePolicyRouting: true,
			},
		}
	}

	if publicDynamic == nil && networkComponents.PrimaryNetworkComponent.NetworkVlan.Id != 0 {
		networks["generated-public"] = Network{
			Type:          "dynamic",
			IP:            networkComponents.PrimaryNetworkComponent.PrimaryIpAddress,
			Preconfigured: true,
			CloudProperties: NetworkCloudProperties{
				VlanID:              networkComponents.PrimaryNetworkComponent.NetworkVlan.Id,
				SourcePolicyRouting: true,
			},
		}
	}

	return networks, nil
}

func (u *Softlayer_Ubuntu_Net) ComponentByNetworkName(components datatypes.SoftLayer_Virtual_Guest, networks Networks) (map[string]datatypes.SoftLayer_Virtual_Guest_Network_Component, error) {
	componentByNetwork := map[string]datatypes.SoftLayer_Virtual_Guest_Network_Component{}

	for name, network := range networks {
		switch network.CloudProperties.VlanID {
		case components.PrimaryBackendNetworkComponent.NetworkVlan.Id:
			componentByNetwork[name] = *components.PrimaryBackendNetworkComponent
		case components.PrimaryNetworkComponent.NetworkVlan.Id:
			componentByNetwork[name] = *components.PrimaryNetworkComponent
		default:
			return nil, fmt.Errorf("Network %q specified a vlan that is not associated with this virtual guest", name)
		}
	}

	return componentByNetwork, nil
}

//go:generate counterfeiter -o fakes/fake_link_namer.go --fake-name FakeLinkNamer . LinkNamer
type LinkNamer interface {
	Name(interfaceName, networkName string) (string, error)
}

type indexedNamer struct {
	indices map[string]int
}

func NewIndexedNamer(networks Networks) LinkNamer {
	indices := map[string]int{}

	index := 0
	for name := range networks {
		indices[name] = index
		index++
	}

	return &indexedNamer{
		indices: indices,
	}
}

func (l *indexedNamer) Name(interfaceName, networkName string) (string, error) {
	idx, ok := l.indices[networkName]
	if !ok {
		return "", fmt.Errorf("Network name not found: %q", networkName)
	}

	return fmt.Sprintf("%s:%d", interfaceName, idx), nil
}