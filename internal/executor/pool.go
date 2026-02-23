package executor

import (
	"context"
	"sync"

	"jenx/internal/jenkins"
	"jenx/internal/models"
)

func Run(ctx context.Context, client *jenkins.Client, jobURL string, specs []models.JobSpec, concurrency int, out chan<- models.RunUpdate) {
	defer close(out)
	if concurrency < 1 {
		concurrency = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				spec := specs[idx]
				out <- models.RunUpdate{Index: idx, State: models.RunQueued}
				queueURL, err := client.TriggerBuild(ctx, jobURL, spec.Params)
				if err != nil {
					out <- models.RunUpdate{Index: idx, State: models.RunError, Err: err, Done: true}
					continue
				}
				out <- models.RunUpdate{Index: idx, State: models.RunQueued, QueueURL: queueURL}

				buildURL, num, err := client.ResolveQueue(ctx, queueURL)
				if err != nil {
					out <- models.RunUpdate{Index: idx, State: models.RunError, QueueURL: queueURL, Err: err, Done: true}
					continue
				}
				out <- models.RunUpdate{Index: idx, State: models.RunRunning, QueueURL: queueURL, BuildURL: buildURL, BuildNumber: num}

				result, err := client.PollBuild(ctx, buildURL)
				if err != nil {
					out <- models.RunUpdate{Index: idx, State: models.RunError, BuildURL: buildURL, BuildNumber: num, Err: err, Done: true}
					continue
				}
				state := mapResult(result)
				out <- models.RunUpdate{Index: idx, State: state, BuildURL: buildURL, BuildNumber: num, Result: result, Done: true}
			}
		}()
	}

	for i := range specs {
		select {
		case <-ctx.Done():
			break
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
}

func mapResult(result string) models.RunState {
	switch result {
	case "SUCCESS":
		return models.RunSuccess
	case "ABORTED":
		return models.RunAborted
	default:
		return models.RunFailed
	}
}
