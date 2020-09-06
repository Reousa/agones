// Copyright 2020 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"testing"
	"time"

	agonesv1 "agones.dev/agones/pkg/apis/agones/v1"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestGameServerUnhealthyAfterDeletingPodWhileControllerDown(t *testing.T) {
	logger := logrus.WithField("test", t.Name())
	gs := framework.DefaultGameServer(defaultNs)
	readyGs, err := framework.CreateGameServerAndWaitUntilReady(defaultNs, gs)
	if err != nil {
		t.Fatalf("Could not get a GameServer ready: %v", err)
	}
	logger.WithField("gsKey", readyGs.ObjectMeta.Name).Info("GameServer Ready")

	gsClient := framework.AgonesClient.AgonesV1().GameServers(defaultNs)
	podClient := framework.KubeClient.CoreV1().Pods(defaultNs)
	defer gsClient.Delete(readyGs.ObjectMeta.Name, nil) // nolint: errcheck

	pod, err := podClient.Get(readyGs.ObjectMeta.Name, metav1.GetOptions{})
	assert.NoError(t, err)
	assert.True(t, metav1.IsControlledBy(pod, readyGs))

	err = deleteAgonesControllerPods()
	assert.NoError(t, err)
	err = podClient.Delete(pod.ObjectMeta.Name, nil)
	assert.NoError(t, err)

	_, err = framework.WaitForGameServerState(readyGs, agonesv1.GameServerStateUnhealthy, 3*time.Minute)
	assert.NoError(t, err)
	logger.Info("waiting for Agones controller to come back to running")
	assert.NoError(t, waitForAgonesControllerRunning())
}

// deleteAgonesControllerPods deletes all the Controller pods for the Agones controller,
// faking a controller crash.
func deleteAgonesControllerPods() error {
	list, err := getAgonesControllerPods()
	if err != nil {
		return err
	}

	policy := metav1.DeletePropagationBackground
	for i := range list.Items {
		err = framework.KubeClient.CoreV1().Pods("agones-system").Delete(list.Items[i].ObjectMeta.Name,
			&metav1.DeleteOptions{PropagationPolicy: &policy})
		if err != nil {
			return err
		}
	}
	return nil
}

func waitForAgonesControllerRunning() error {
	return wait.PollImmediate(time.Second, 5*time.Minute, func() (bool, error) {
		list, err := getAgonesControllerPods()
		if err != nil {
			return true, err
		}

		for i := range list.Items {
			for _, c := range list.Items[i].Status.ContainerStatuses {
				if c.State.Running == nil {
					return false, nil
				}
			}
		}

		return true, nil
	})
}

// getAgonesControllerPods returns all the Agones controller pods
func getAgonesControllerPods() (*corev1.PodList, error) {
	opts := metav1.ListOptions{LabelSelector: labels.Set{"agones.dev/role": "controller"}.String()}
	return framework.KubeClient.CoreV1().Pods("agones-system").List(opts)
}
