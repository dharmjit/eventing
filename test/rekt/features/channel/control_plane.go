/*
Copyright 2021 The Knative Authors

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

package channel

import (
	"context"

	"github.com/google/go-cmp/cmp"
	duckv1 "knative.dev/eventing/pkg/apis/duck/v1"
	"knative.dev/eventing/pkg/apis/messaging"
	"knative.dev/eventing/test/rekt/features/knconf"
	"knative.dev/eventing/test/rekt/resources/account_role"
	"knative.dev/eventing/test/rekt/resources/channel_impl"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	"knative.dev/reconciler-test/pkg/feature"
)

func ControlPlaneConformance(channelName string) *feature.FeatureSet {
	fs := &feature.FeatureSet{
		Name: "Knative Channel Specification - Control Plane",
		Features: []feature.Feature{
			*ControlPlaneChannel(channelName),
		},
	}

	return fs
}

func ControlPlaneChannel(channelName string) *feature.Feature {
	f := feature.NewFeatureNamed("Conformance")

	f.Setup("Set Channel Name", setChannelableName(channelName))

	sacmName := feature.MakeRandomK8sName("channelable-manipulator")
	f.Setup("Create Service Account for Channelable Manipulator",
		account_role.Install(sacmName, account_role.AsChannelableManipulator))

	saarName := feature.MakeRandomK8sName("addressale-resolver")
	f.Setup("Create Service Account for Addressable Resolver",
		account_role.Install(saarName, account_role.AsAddressableResolver))

	f.Stable("Aggregated Channelable Manipulator ClusterRole").
		Must("Every CRD MUST create a corresponding ClusterRole, that will be aggregated into the channelable-manipulator ClusterRole.",
			serviceAccountIsChannelableManipulator(sacmName)).
		Must("This ClusterRole MUST include permissions to create, get, list, watch, patch, and update the CRD's custom objects and their status.",
			noop). // Tested by serviceAccountIsChannelableManipulator
		Must("Each channel MUST have the duck.knative.dev/channelable: \"true\" label on its channelable-manipulator ClusterRole.",
			noop) // Tested by serviceAccountIsChannelableManipulator

	f.Stable("Aggregated Addressable Resolver ClusterRole").
		Must("Every CRD MUST create a corresponding ClusterRole, that will be aggregated into the addressable-resolver ClusterRole.",
			serviceAccountIsAddressableResolver(saarName)).
		Must("This ClusterRole MUST include permissions to get, list, and watch the CRD's custom objects and their status.",
			noop). // Tested by serviceAccountIsAddressableResolver
		Must("Each channel MUST have the duck.knative.dev/addressable: \"true\" label on its addressable-resolver ClusterRole.",
			noop) // Tested by serviceAccountIsAddressableResolver

	f.Stable("CustomResourceDefinition per Channel").
		Must("Each channel is namespaced", crdOfChannelIsNamespaced).
		Must("label of messaging.knative.dev/subscribable: true",
			crdOfChannelIsLabeled(messaging.SubscribableDuckVersionAnnotation, "true")).
		Must("label of duck.knative.dev/addressable: true",
			crdOfChannelIsLabeled(duck.AddressableDuckVersionLabel, "true")).
		Must("The category `channel`", crdOfChannelHasCategory("channel"))

	f.Stable("Annotation Requirements").
		Should("each instance SHOULD have annotation: messaging.knative.dev/subscribable: v1",
			channelHasAnnotations)

	f.Stable("Spec Requirements").
		Must("Each channel CRD MUST contain an array of subscribers: spec.subscribers",
			channelAllowsSubscribers)
		// Special note for Channel tests: The array of subscribers MUST NOT be
		// set directly on the generic Channel custom object, but rather
		// appended to the backing channel by the subscription itself.

	f.Stable("Status Requirements").
		Must("Each channel CRD MUST have a status subresource which contains [address]",
			noop). // tested by readyChannelIsAddressable
		Must("Each channel CRD MUST have a status subresource which contains [subscribers (as an array)]", todo).
		Should("SHOULD have in status observedGeneration",
			noop). // tested by knconf.KResourceHasReadyInConditions
		Must("observedGeneration MUST be populated if present",
			noop). // tested by knconf.KResourceHasReadyInConditions
		Should("SHOULD have in status conditions (as an array)",
			knconf.KResourceHasReadyInConditions(channel_impl.GVR(), channelName)).
		Should("status.conditions SHOULD indicate status transitions and error reasons if present",
			todo) // how to test this?

	f.Stable("Channel Status").
		Must("When the channel instance is ready to receive events status.address.url MUST be populated",
			readyChannelIsAddressable).
		Must("When the channel instance is ready to receive events status.address.url status.addressable MUST be set to True",
			noop) // tested by readyChannelIsAddressable

	f.Stable("Channel Subscriber Status").
		Must("The ready field of the subscriber identified by its uid MUST be set to True when the subscription is ready to be processed",
			todo)

	return f
}

func serviceAccountIsChannelableManipulator(name string) feature.StepFn {
	return func(ctx context.Context, t feature.T) {
		gvr := channel_impl.GVR()
		for _, verb := range []string{"get", "list", "watch", "update", "patch"} {
			ServiceAccountSubjectAccessReviewAllowedOrFail(ctx, t, gvr, "", name, verb)
			ServiceAccountSubjectAccessReviewAllowedOrFail(ctx, t, gvr, "status", name, verb)
		}

	}
}

func serviceAccountIsAddressableResolver(name string) feature.StepFn {
	return func(ctx context.Context, t feature.T) {
		gvr := channel_impl.GVR()
		for _, verb := range []string{"get", "list", "watch"} {
			ServiceAccountSubjectAccessReviewAllowedOrFail(ctx, t, gvr, "", name, verb)
			ServiceAccountSubjectAccessReviewAllowedOrFail(ctx, t, gvr, "status", name, verb)
		}

	}
}

func channelHasAnnotations(ctx context.Context, t feature.T) {
	ch := getChannelable(ctx, t)
	if version, found := ch.Annotations["messaging.knative.dev/subscribable"]; !found {
		t.Error(`expected annotations["messaging.knative.dev/subscribable"] to exist`)
	} else if version != "v1" {
		t.Error(`expected "messaging.knative.dev/subscribable" to be "v1", found`, version)
	}
}

func channelAllowsSubscribers(ctx context.Context, t feature.T) {
	ch := getChannelable(ctx, t)
	original := ch.DeepCopy()

	u, _ := apis.ParseURL("http://example.com")
	want := duckv1.SubscriberSpec{
		UID:           "abc123",
		Generation:    1,
		SubscriberURI: u,
	}

	ch.Spec.Subscribers = append(ch.Spec.Subscribers, want)
	patchChannelable(ctx, t, original, ch)

	updated := getChannelable(ctx, t)
	if len(updated.Spec.Subscribers) <= 0 {
		t.Errorf("subscriber was not saved")
	}

	found := false
	for _, got := range updated.Spec.Subscribers {
		if got.UID == want.UID {
			found = true
			if diff := cmp.Diff(want, got); diff != "" {
				t.Error("Round trip Subscriber has a delta, (-want, +got) =", diff)
			}
		}
	}
	if !found {
		t.Error("Round trip Subscriber failed.")
	}
}

func readyChannelIsAddressable(ctx context.Context, t feature.T) {
	channel := getChannelable(ctx, t)

	if c := channel.Status.GetCondition(apis.ConditionReady); c.IsTrue() {
		if channel.Status.Address.URL == nil {
			t.Errorf("channel is not addressable")
		}
		// Success!
	} else {
		t.Errorf("channel was not ready")
	}
}