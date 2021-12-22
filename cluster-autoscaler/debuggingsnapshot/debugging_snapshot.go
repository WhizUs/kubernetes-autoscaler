/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package debuggingsnapshot

import (
	"encoding/json"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

// NodeInfo captures a single entity of nodeInfo. i.e. Node specs and all the pods on that node.
type NodeInfo struct {
	Node *v1.Node             `json:"Node"`
	Pods []*framework.PodInfo `json:"Pods"`
}

// DebuggingSnapshot is the interface used to define any debugging snapshot
// implementation, incl. any custom impl. to be used by DebuggingSnapshotter
type DebuggingSnapshot interface {
	// SetNodeGroupInfo is a setter to capture all the NodeInfo
	SetNodeGroupInfo([]*framework.NodeInfo)
	// SetUnscheduledPodsCanBeScheduled is a setter for all pods which are unscheduled
	// but they can be scheduled. i.e. pods which aren't triggering scale-up
	SetUnscheduledPodsCanBeScheduled([]*v1.Pod)
	// SetErrorMessage sets the error message in the snapshot
	SetErrorMessage(string)
	// SetEndTimestamp sets the timestamp in the snapshot,
	// when all the data collection is finished
	SetEndTimestamp(time.Time)
	// SetStartTimestamp sets the timestamp in the snapshot,
	// when all the data collection is started
	SetStartTimestamp(time.Time)
	// GetOutputBytes return the output state of the Snapshot with bool to specify if
	// the snapshot has the error message set
	GetOutputBytes() ([]byte, bool)
	// Cleanup clears the internal data obj of the snapshot, readying for next request
	Cleanup()
}

// DebuggingSnapshotImpl is the struct used to collect all the data to be output.
// Please add all new output fields in this struct. This is to make the data
// encoding/decoding easier as the single object going into the decoder
type DebuggingSnapshotImpl struct {
	NodeInfo                      []*NodeInfo `json:"NodeList"`
	UnscheduledPodsCanBeScheduled []*v1.Pod   `json:"UnscheduledPodsCanBeScheduled"`
	Error                         string      `json:"Error,omitempty"`
	StartTimestamp                time.Time   `json:"StartTimestamp"`
	EndTimestamp                  time.Time   `json:"EndTimestamp"`
}

// SetUnscheduledPodsCanBeScheduled is the setter for UnscheduledPodsCanBeScheduled
func (s *DebuggingSnapshotImpl) SetUnscheduledPodsCanBeScheduled(podList []*v1.Pod) {
	if podList == nil {
		return
	}

	s.UnscheduledPodsCanBeScheduled = nil
	for _, pod := range podList {
		s.UnscheduledPodsCanBeScheduled = append(s.UnscheduledPodsCanBeScheduled, pod)
	}
}

// SetNodeGroupInfo is the setter for Node Group Info
// All filtering/prettifying of data should be done here.
func (s *DebuggingSnapshotImpl) SetNodeGroupInfo(nodeInfos []*framework.NodeInfo) {
	if nodeInfos == nil {
		return
	}

	var NodeInfoList []*NodeInfo

	for _, n := range nodeInfos {
		nClone := n.Clone()
		node := nClone.Node()

		nodeInfo := &NodeInfo{
			Node: node,
			Pods: nClone.Pods,
		}

		NodeInfoList = append(NodeInfoList, nodeInfo)
	}
	s.NodeInfo = NodeInfoList
}

// SetEndTimestamp is the setter for end timestamp
func (s *DebuggingSnapshotImpl) SetEndTimestamp(t time.Time) {
	s.EndTimestamp = t
}

// SetStartTimestamp is the setter for end timestamp
func (s *DebuggingSnapshotImpl) SetStartTimestamp(t time.Time) {
	s.StartTimestamp = t
}

// GetOutputBytes return the output state of the Snapshot with bool to specify if
// the snapshot has the error message set
func (s *DebuggingSnapshotImpl) GetOutputBytes() ([]byte, bool) {
	errMsgSet := false
	if s.Error != "" {
		klog.Errorf("Debugging snapshot found with error message set when GetOutputBytes() is called. - ", s.Error)
		errMsgSet = true
	}

	klog.Infof("Debugging snapshot flush ready")
	marshalOutput, err := json.Marshal(s)

	// this error captures if the snapshot couldn't be marshalled, hence we create a new object
	// and return the error message
	if err != nil {
		klog.Errorf("Unable to json marshal the debugging snapshot: %v", err)
		errorSnapshot := DebuggingSnapshotImpl{}
		errorSnapshot.SetErrorMessage("Unable to marshal the snapshot, " + err.Error())
		errorSnapshot.SetEndTimestamp(s.EndTimestamp)
		errorSnapshot.SetStartTimestamp(s.StartTimestamp)
		errorMarshal, err1 := json.Marshal(errorSnapshot)
		klog.Errorf("Unable to marshal a new Debugging Snapshot Impl, with just a error message: %v", err1)
		return errorMarshal, true
	}

	return marshalOutput, errMsgSet
}

// SetErrorMessage sets the error message in the snapshot
func (s *DebuggingSnapshotImpl) SetErrorMessage(error string) {
	s.Error = error
}

// Cleanup cleans up all the data in the snapshot without changing the
// pointer reference
func (s *DebuggingSnapshotImpl) Cleanup() {
	*s = DebuggingSnapshotImpl{}
}
