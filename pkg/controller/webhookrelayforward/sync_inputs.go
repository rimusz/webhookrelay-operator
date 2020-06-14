package webhookrelayforward

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/webhookrelay/webhookrelay-go"

	forwardv1 "github.com/webhookrelay/webhookrelay-operator/pkg/apis/forward/v1"
)

// ensureBucketInputs checks and configures input specific information
func (r *ReconcileWebhookRelayForward) ensureBucketInputs(logger logr.Logger, instance *forwardv1.WebhookRelayForward, bucketSpec *forwardv1.BucketSpec) error {

	// If no inputs are defined, nothing to do
	if len(bucketSpec.Inputs) == 0 {
		return nil
	}

	bucket, ok := r.apiClient.bucketsCache.Get(bucketSpec.Name)
	if !ok {
		return fmt.Errorf("bucket '%s' not found in the cache, will wait for the next reconcile loop", bucketSpec.Name)
	}

	logger = logger.WithValues(
		"bucket_name", bucket.Name,
		"bucket_id", bucket.ID,
	)

	// Create a list of desired inputs and then diff existing
	// ones against them to build a list of what inputs
	// we should create, update and which ones to delete
	desired := desiredInputs(bucketSpec, bucket)
	diff := getInputsDiff(bucket.Inputs, desired)

	var err error

	// Create inputs that need to be created
	for idx := range diff.create {
		_, err = r.apiClient.client.CreateInput(diff.create[idx])
		if err != nil {
			logger.Error(err, "failed to create input")
		}
	}

	for idx := range diff.create {
		_, err = r.apiClient.client.CreateInput(diff.create[idx])
		if err != nil {
			logger.Error(err, "failed to create input")
		}
	}

	for idx := range diff.update {
		_, err = r.apiClient.client.UpdateInput(diff.update[idx])
		if err != nil {
			logger.Error(err, "failed to update input",
				"input_id", diff.update[idx].ID,
			)
		}
	}

	for idx := range diff.delete {
		err = r.apiClient.client.DeleteInput(&webhookrelay.InputDeleteOptions{
			Bucket: diff.delete[idx].BucketID,
			Input:  diff.delete[idx].ID,
		})
		if err != nil {
			logger.Error(err, "failed to delete input",
				"input_id", diff.update[idx].ID,
			)
		}
	}

	return nil
}

func desiredInputs(bucketSpec *forwardv1.BucketSpec, bucket *webhookrelay.Bucket) []*webhookrelay.Input {

	return nil
}

func getInputsDiff(current, desired []*webhookrelay.Input) *inputsDiff {

	diff := &inputsDiff{}

	return diff
}

type inputsDiff struct {
	create []*webhookrelay.Input
	update []*webhookrelay.Input
	delete []*webhookrelay.Input
}
