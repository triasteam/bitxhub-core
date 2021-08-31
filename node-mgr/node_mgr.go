package node_mgr

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/looplab/fsm"
	"github.com/meshplus/bitxhub-core/governance"
	"github.com/sirupsen/logrus"
)

type NodeType string

const (
	NODEPREFIX        = "node"
	VP_NODE_ID_PREFIX = "vp-id"

	VPNode  NodeType = "vpNode"
	NVPNode NodeType = "nvpNode"
)

type NodeManager struct {
	governance.Persister
}

type Node struct {
	Pid      string   `toml:"pid" json:"pid"`
	Account  string   `toml:"account" json:"account"`
	NodeType NodeType `toml:"node_type" json:"node_type"`

	// VP Node Info
	VPNodeId uint64 `toml:"id" json:"id"`
	Primary  bool   `toml:"primary" json:"primary"`

	Status governance.GovernanceStatus `toml:"status" json:"status"`
	FSM    *fsm.FSM                    `json:"fsm"`
}

var nodeAvailableMap = map[governance.GovernanceStatus]struct{}{
	governance.GovernanceAvailable: {},
	governance.GovernanceLogouting: {},
}

var nodeStateMap = map[governance.EventType][]governance.GovernanceStatus{
	governance.EventRegister: {governance.GovernanceUnavailable},
	governance.EventLogout:   {governance.GovernanceAvailable},
}

func New(persister governance.Persister) NodeMgr {
	return &NodeManager{persister}
}

func (n *Node) IsAvailable() bool {
	if _, ok := nodeAvailableMap[n.Status]; ok {
		return true
	} else {
		return false
	}
}

func (node *Node) setFSM(lastStatus governance.GovernanceStatus) {
	node.FSM = fsm.NewFSM(
		string(node.Status),
		fsm.Events{
			// register 3
			{Name: string(governance.EventRegister), Src: []string{string(governance.GovernanceUnavailable)}, Dst: string(governance.GovernanceRegisting)},
			{Name: string(governance.EventApprove), Src: []string{string(governance.GovernanceRegisting)}, Dst: string(governance.GovernanceAvailable)},
			{Name: string(governance.EventReject), Src: []string{string(governance.GovernanceRegisting)}, Dst: string(lastStatus)},

			// logout 3
			{Name: string(governance.EventLogout), Src: []string{string(governance.GovernanceAvailable)}, Dst: string(governance.GovernanceLogouting)},
			{Name: string(governance.EventApprove), Src: []string{string(governance.GovernanceLogouting)}, Dst: string(governance.GovernanceUnavailable)},
			{Name: string(governance.EventReject), Src: []string{string(governance.GovernanceLogouting)}, Dst: string(lastStatus)},
		},
		fsm.Callbacks{
			"enter_state": func(e *fsm.Event) {
				node.Status = governance.GovernanceStatus(node.FSM.Current())
			},
		},
	)
}

// GovernancePre checks if the appchain can do the event. (only check, not modify infomation)
// return *node, extra info, error
func (nm *NodeManager) GovernancePre(nodePid string, event governance.EventType, _ []byte) (interface{}, error) {
	node := &Node{}
	if ok := nm.GetObject(nm.nodeKey(nodePid), node); !ok {
		if event == governance.EventRegister {
			return nil, nil
		} else {
			return nil, fmt.Errorf("the node does not exist")
		}
	}

	for _, s := range nodeStateMap[event] {
		if node.Status == s {
			return node, nil
		}
	}

	return nil, fmt.Errorf("the node (%s) can not be %s", string(node.Status), string(event))
}

func (nm *NodeManager) ChangeStatus(nodePid string, trigger, lastStatus string, _ []byte) (bool, []byte) {
	node := &Node{}
	if ok := nm.GetObject(nm.nodeKey(nodePid), node); !ok {
		return false, []byte("this node does not exist")
	}

	node.setFSM(governance.GovernanceStatus(lastStatus))
	err := node.FSM.Event(trigger)
	if err != nil {
		return false, []byte(fmt.Sprintf("change status error: %v", err))
	}

	nm.SetObject(nm.nodeKey(nodePid), *node)
	return true, nil
}

// Register record node info
func (nm *NodeManager) Register(nodeInfo []byte) (bool, []byte) {
	node := &Node{}
	if err := json.Unmarshal(nodeInfo, node); err != nil {
		return false, []byte(err.Error())
	}

	nm.SetObject(nm.nodeKey(node.Pid), node)
	nm.SetObject(nm.vpNodeIdKey(strconv.Itoa(int(node.VPNodeId))), node.Pid)
	nm.Logger().WithFields(logrus.Fields{
		"pid":      node.Pid,
		"nodeType": node.NodeType,
	}).Info("Node is registering")

	return true, nil
}

// CountAvailable counts all available nodes (available、logouting)
func (nm *NodeManager) CountAvailable(nodeType []byte) (bool, []byte) {
	ok, value := nm.Query(NODEPREFIX)
	if !ok {
		return true, []byte("0")
	}

	count := 0
	for _, v := range value {
		node := &Node{}
		if err := json.Unmarshal(v, node); err != nil {
			return false, []byte(fmt.Sprintf("unmarshal json error: %v", err))
		}
		if node.NodeType == NodeType(nodeType) {
			if node.IsAvailable() {
				count++
			}
		}
	}
	return true, []byte(strconv.Itoa(count))
}

func (nm *NodeManager) CountAll(nodeType []byte) (bool, []byte) {
	ok, value := nm.Query(NODEPREFIX)
	if !ok {
		return true, []byte("0")
	}

	count := 0
	for _, v := range value {
		node := &Node{}
		if err := json.Unmarshal(v, node); err != nil {
			return false, []byte(fmt.Sprintf("unmarshal json error: %v", err))
		}
		if node.NodeType == NodeType(nodeType) {
			count++
		}
	}
	return true, []byte(strconv.Itoa(count))
}

// All returns all nodes
func (nm *NodeManager) All(_ []byte) (interface{}, error) {
	ok, value := nm.Query(NODEPREFIX)
	if !ok {
		return nil, nil
	}

	ret := make([]*Node, 0)
	for _, data := range value {
		node := &Node{}
		if err := json.Unmarshal(data, node); err != nil {
			return nil, err
		}
		ret = append(ret, node)
	}

	return ret, nil
}

func (nm *NodeManager) QueryById(nodePid string, _ []byte) (interface{}, error) {
	var node Node
	ok := nm.GetObject(nm.nodeKey(nodePid), &node)
	if !ok {
		return nil, fmt.Errorf("this node does not exist")
	}

	return &node, nil
}

func (nm *NodeManager) GetPidById(nodeId string) (string, error) {
	ok, data := nm.Get(nm.vpNodeIdKey(nodeId))
	if !ok {
		return "", fmt.Errorf("this node does not exist")
	}

	return string(data), nil
}

func (nm *NodeManager) nodeKey(pid string) string {
	return fmt.Sprintf("%s-%s", NODEPREFIX, pid)
}

func (nm *NodeManager) vpNodeIdKey(id string) string {
	return fmt.Sprintf("%s-%s", VP_NODE_ID_PREFIX, id)
}
