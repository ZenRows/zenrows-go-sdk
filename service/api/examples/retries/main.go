package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

const (
	maxConcurrentRequests = 5                // run 5 scraping requests at the same time
	totalRequests         = 10               // send a total of 10 scraping requests
	maxRetryCount         = 5                // retry up to five times
	retryWait             = 20 * time.Second // waiting at least 20s between retries (just for demonstration purposes!)
	retryMaxWait          = 25 * time.Second // and waiting a maximum of 25s between retries
)

func main() {
	client := scraperapi.NewClient(
		scraperapi.WithAPIKey("YOUR_API_KEY"),
		scraperapi.WithMaxConcurrentRequests(maxConcurrentRequests),
		scraperapi.WithMaxRetryCount(maxRetryCount),
		scraperapi.WithRetryWaitTime(retryWait),
		scraperapi.WithRetryMaxWaitTime(retryMaxWait),
	)

	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now := time.Now() // store the time, to be able to print the elapsed duration

			// target the https://httpbin.io/unstable endpoint, as it fails half of the times, so the retry mechanism takes care of
			// making sure we eventually receive a successful request
			res, err := client.Get(context.Background(), "https://httpbin.io/unstable", &scraperapi.RequestParameters{})
			if err != nil {
				fmt.Println(i, err)
				return
			}

			if err = res.Error(); err != nil {
				fmt.Println(i, err)
				return
			}

			fmt.Printf("[#%d]: %s (in %s)\n", i, res.Status(), time.Since(now))
		}(i)
	}

	wg.Wait()
	fmt.Println("done")
}
