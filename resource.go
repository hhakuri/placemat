package menu

import "net"

// NodeType represent node type(i.g. boot, CS, SS)
type NodeType int

const (
	// BootNode represent node type of boot server
	BootNode NodeType = iota
	// CSNode represent node type of compute server
	CSNode
	// SSNode represent node type of storage server
	SSNode
)

// NetworkMenu represents network settings to be written to the configuration file
type NetworkMenu struct {
	ASNBase      int
	External     *net.IPNet
	SpineTor     net.IP
	Node         *net.IPNet
	Bastion      *net.IPNet
	LoadBalancer *net.IPNet
	Ingress      *net.IPNet
}

// InventoryMenu represents inventory settings to be written to the configuration file
type InventoryMenu struct {
	Spine int
	Rack  []RackMenu
}

// RackMenu represents how many nodes each rack contains
type RackMenu struct {
	CS int
	SS int
}

// NodeMenu represents computing resources used by each type nodes
type NodeMenu struct {
	Type   NodeType
	CPU    int
	Memory string
}

// AccountMenu contains user account settings
type AccountMenu struct {
	UserName string
	Password string
}

// Menu is a top-level structure that summarizes the settings of each menus
type Menu struct {
	Network   *NetworkMenu
	Inventory *InventoryMenu
	Nodes     []*NodeMenu
	Account   *AccountMenu
}
