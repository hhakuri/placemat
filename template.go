package menu

import (
	"errors"
	"fmt"
	"net"

	"github.com/cybozu-go/netutil"
)

const (
	torPerRack = 2

	offsetInternetHost       = 1
	offsetInternetCoreRouter = 2

	offsetExternalCoreRouter = 1
	offsetExternalExtVM      = 2

	offsetBastionCoreRouter = 1
	offsetBastionOperation  = 2

	offsetNodenetToR     = 1
	offsetNodenetBoot    = 3
	offsetNodenetServers = 4

	offsetASNCoreRouter = -3
	offsetASNExtVM      = -2
	offsetASNSpine      = -1
)

// Rack is template args for rack
type Rack struct {
	Name                  string
	ShortName             string
	ASN                   int
	NodeNetworkPrefixSize int
	ToR1                  ToR
	ToR2                  ToR
	BootNode              BootNodeEntity
	CSList                []Node
	SSList                []Node
	node0Network          *net.IPNet
	node1Network          *net.IPNet
	node2Network          *net.IPNet
}

// Node is a template args for a node
type Node struct {
	Name         string
	Node0Address *net.IPNet
	Node1Address *net.IPNet
	Node2Address *net.IPNet
	ToR1Address  *net.IPNet
	ToR2Address  *net.IPNet
}

// ToR is a template args for a ToR switch
type ToR struct {
	Name           string
	SpineAddresses []*net.IPNet
	NodeAddress    *net.IPNet
	NodeInterface  string
}

// BootNodeEntity is a template args for a boot node
type BootNodeEntity struct {
	Node

	BastionAddress *net.IPNet
}

// Spine is a template args for Spine
type Spine struct {
	Name              string
	ShortName         string
	CoreRouterAddress *net.IPNet
	ToRAddresses      []*net.IPNet
}

// ToR1Address returns spine's IP address connected from ToR-1 in the specified rack
func (s Spine) ToR1Address(rackIdx int) *net.IPNet {
	return s.ToRAddresses[rackIdx*2]
}

// ToR2Address returns spine's IP address connected from ToR-2 in the specified rack
func (s Spine) ToR2Address(rackIdx int) *net.IPNet {
	return s.ToRAddresses[rackIdx*2+1]
}

// Endpoints contains endpoints for external hosts
type Endpoints struct {
	Host      *net.IPNet
	ExtVM     *net.IPNet
	Operation *net.IPNet
}

// CoreRouter contains parameters to construct core router
type CoreRouter struct {
	InternetAddress *net.IPNet
	SpineAddresses  []*net.IPNet
	BastionAddress  *net.IPNet
	ExtVMAddress    *net.IPNet
}

// TemplateArgs is args for cluster.yml
type TemplateArgs struct {
	Network struct {
		Exposed struct {
			Bastion      *net.IPNet
			LoadBalancer *net.IPNet
			Ingress      *net.IPNet
		}
		Endpoints     Endpoints
		ASNExtVM      int
		ASNSpine      int
		ASNCoreRouter int
	}
	Racks      []Rack
	Spines     []Spine
	CoreRouter CoreRouter
	CS         VMResource
	SS         VMResource
	Boot       VMResource
	Account    Account
}

// Account is setting data to create linux user account
type Account struct {
	Name         string
	PasswordHash string
}

// BIRDRackTemplateArgs is args to generate bird config for each rack
type BIRDRackTemplateArgs struct {
	Args    TemplateArgs
	RackIdx int
}

// BIRDSpineTemplateArgs is args to generate bird config for each spine
type BIRDSpineTemplateArgs struct {
	Args     TemplateArgs
	SpineIdx int
}

// VMResource is args to specify vm resource
type VMResource struct {
	CPU    int
	Memory string
}

// ToTemplateArgs is converter Menu to TemplateArgs
func ToTemplateArgs(menu *Menu) (*TemplateArgs, error) {
	var templateArgs TemplateArgs
	templateArgs.Account.Name = menu.Account.UserName
	templateArgs.Account.PasswordHash = menu.Account.PasswordHash

	setNetworkArgs(&templateArgs, menu)

	for _, node := range menu.Nodes {
		switch node.Type {
		case CSNode:
			templateArgs.CS.Memory = node.Memory
			templateArgs.CS.CPU = node.CPU
		case SSNode:
			templateArgs.SS.Memory = node.Memory
			templateArgs.SS.CPU = node.CPU
		case BootNode:
			templateArgs.Boot.Memory = node.Memory
			templateArgs.Boot.CPU = node.CPU
		default:
			return nil, errors.New("invalid node type")
		}
	}

	numRack := len(menu.Inventory.Rack)

	spineToRackBases := make([][]net.IP, menu.Inventory.Spine)
	spineTorInt := netutil.IP4ToInt(menu.Network.SpineTor)
	for spineIdx := 0; spineIdx < menu.Inventory.Spine; spineIdx++ {
		spineToRackBases[spineIdx] = make([]net.IP, numRack)
		for rackIdx := range menu.Inventory.Rack {
			offset := uint32((spineIdx*numRack + rackIdx) * torPerRack * 2)
			spineToRackBases[spineIdx][rackIdx] = netutil.IntToIP4(spineTorInt + offset)
		}
	}

	templateArgs.Racks = make([]Rack, numRack)
	for rackIdx, rackMenu := range menu.Inventory.Rack {
		rack := &templateArgs.Racks[rackIdx]
		rack.Name = fmt.Sprintf("rack%d", rackIdx)
		rack.ShortName = fmt.Sprintf("r%d", rackIdx)
		rack.ASN = menu.Network.ASNBase + rackIdx
		rack.node0Network = makeNodeNetwork(menu.Network.Node, rackIdx*3+0)
		rack.node1Network = makeNodeNetwork(menu.Network.Node, rackIdx*3+1)
		rack.node2Network = makeNodeNetwork(menu.Network.Node, rackIdx*3+2)

		constructToRAddresses(rack, rackIdx, menu, spineToRackBases)
		constructBootAddresses(rack, rackIdx, menu)
		prefixSize, _ := menu.Network.Node.Mask.Size()
		rack.NodeNetworkPrefixSize = prefixSize

		for csIdx := 0; csIdx < rackMenu.CS; csIdx++ {
			node := buildNode("cs", csIdx, offsetNodenetServers, rack)
			rack.CSList = append(rack.CSList, node)
		}
		for ssIdx := 0; ssIdx < rackMenu.SS; ssIdx++ {
			node := buildNode("ss", ssIdx, offsetNodenetServers+rackMenu.CS, rack)
			rack.SSList = append(rack.SSList, node)
		}
	}

	templateArgs.Spines = make([]Spine, menu.Inventory.Spine)
	for spineIdx := 0; spineIdx < menu.Inventory.Spine; spineIdx++ {
		spine := &templateArgs.Spines[spineIdx]
		spine.Name = fmt.Sprintf("spine%d", spineIdx+1)
		spine.ShortName = fmt.Sprintf("s%d", spineIdx+1)

		spine.CoreRouterAddress = addToIPNet(menu.Network.CoreSpine, (2*spineIdx)+1)
		// {internet} + {tor per rack} * {rack}
		spine.ToRAddresses = make([]*net.IPNet, torPerRack*numRack)
		for rackIdx := range menu.Inventory.Rack {
			spine.ToRAddresses[rackIdx*torPerRack] = addToIP(spineToRackBases[spineIdx][rackIdx], 0, 31)
			spine.ToRAddresses[rackIdx*torPerRack+1] = addToIP(spineToRackBases[spineIdx][rackIdx], 2, 31)
		}
	}

	setCoreRouter(&templateArgs, menu)
	return &templateArgs, nil
}

func setNetworkArgs(templateArgs *TemplateArgs, menu *Menu) {
	templateArgs.Network.ASNCoreRouter = menu.Network.ASNBase + offsetASNCoreRouter
	templateArgs.Network.ASNExtVM = menu.Network.ASNBase + offsetASNExtVM
	templateArgs.Network.ASNSpine = menu.Network.ASNBase + offsetASNSpine
	templateArgs.Network.Exposed.Bastion = menu.Network.Bastion
	templateArgs.Network.Exposed.LoadBalancer = menu.Network.LoadBalancer
	templateArgs.Network.Exposed.Ingress = menu.Network.Ingress
	templateArgs.Network.Endpoints.Host = addToIPNet(menu.Network.Internet, offsetInternetHost)
	templateArgs.Network.Endpoints.ExtVM = addToIPNet(menu.Network.CoreExtVM, offsetExternalExtVM)
	templateArgs.Network.Endpoints.Operation = addToIPNet(menu.Network.Bastion, offsetBastionOperation)
}

func buildNode(basename string, idx int, offsetStart int, rack *Rack) Node {
	node := Node{}
	node.Name = fmt.Sprintf("%v%d", basename, idx+1)
	offset := offsetStart + idx

	node.Node0Address = addToIP(rack.node0Network.IP, offset, 32)
	node.Node1Address = addToIPNet(rack.node1Network, offset)
	node.Node2Address = addToIPNet(rack.node2Network, offset)
	node.ToR1Address = rack.BootNode.ToR1Address
	node.ToR2Address = rack.BootNode.ToR2Address
	return node
}

func constructBootAddresses(rack *Rack, rackIdx int, menu *Menu) {
	rack.BootNode.Node0Address = addToIP(rack.node0Network.IP, offsetNodenetBoot, 32)
	rack.BootNode.Node1Address = addToIPNet(rack.node1Network, offsetNodenetBoot)
	rack.BootNode.Node2Address = addToIPNet(rack.node2Network, offsetNodenetBoot)
	rack.BootNode.BastionAddress = addToIP(menu.Network.Bastion.IP, rackIdx, 32)

	rack.BootNode.ToR1Address = addToIPNet(rack.node1Network, offsetNodenetToR)
	rack.BootNode.ToR2Address = addToIPNet(rack.node2Network, offsetNodenetToR)
}

func setCoreRouter(ta *TemplateArgs, menu *Menu) {
	for i := range ta.Spines {
		ta.CoreRouter.SpineAddresses = append(ta.CoreRouter.SpineAddresses, addToIPNet(menu.Network.CoreSpine, 2*i))
	}
	ta.CoreRouter.BastionAddress = addToIPNet(menu.Network.CoreBastion, offsetBastionCoreRouter)
	ta.CoreRouter.InternetAddress = addToIPNet(menu.Network.Internet, offsetInternetCoreRouter)
	ta.CoreRouter.ExtVMAddress = addToIPNet(menu.Network.CoreExtVM, offsetExternalCoreRouter)
}

func constructToRAddresses(rack *Rack, rackIdx int, menu *Menu, bases [][]net.IP) {
	rack.ToR1.SpineAddresses = make([]*net.IPNet, menu.Inventory.Spine)
	for spineIdx := 0; spineIdx < menu.Inventory.Spine; spineIdx++ {
		rack.ToR1.SpineAddresses[spineIdx] = addToIP(bases[spineIdx][rackIdx], 1, 31)
	}
	rack.ToR1.NodeAddress = addToIPNet(rack.node1Network, offsetNodenetToR)
	rack.ToR1.NodeInterface = fmt.Sprintf("eth%d", menu.Inventory.Spine)

	rack.ToR2.SpineAddresses = make([]*net.IPNet, menu.Inventory.Spine)
	for spineIdx := 0; spineIdx < menu.Inventory.Spine; spineIdx++ {
		rack.ToR2.SpineAddresses[spineIdx] = addToIP(bases[spineIdx][rackIdx], 3, 31)
	}
	rack.ToR2.NodeAddress = addToIPNet(rack.node2Network, offsetNodenetToR)
	rack.ToR2.NodeInterface = fmt.Sprintf("eth%d", menu.Inventory.Spine)
}

func addToIPNet(netAddr *net.IPNet, offset int) *net.IPNet {
	ipInt := netutil.IP4ToInt(netAddr.IP) + uint32(offset)
	ip4 := netutil.IntToIP4(ipInt)
	mask := netAddr.Mask
	return &net.IPNet{IP: ip4, Mask: mask}
}

func addToIP(netIP net.IP, offset int, prefixSize int) *net.IPNet {
	ipInt := netutil.IP4ToInt(netIP) + uint32(offset)
	ip4 := netutil.IntToIP4(ipInt)
	mask := net.CIDRMask(prefixSize, 32)
	return &net.IPNet{IP: ip4, Mask: mask}
}

func makeNodeNetwork(base *net.IPNet, nodeIdx int) *net.IPNet {
	mask := base.Mask
	prefixSize, _ := mask.Size()
	offset := 1 << uint(32-prefixSize) * nodeIdx
	ipInt := netutil.IP4ToInt(base.IP) + uint32(offset)
	ip4 := netutil.IntToIP4(ipInt)
	return &net.IPNet{IP: ip4, Mask: mask}
}
