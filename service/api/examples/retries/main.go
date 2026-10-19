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
	retryWait             = 2 * time.Second  // the first retry waits up to 2s, the second up to 4s, and so on
	retryMaxWait          = 10 * time.Second // but no wait is longer than 10s
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

			// target the https://httpbin.io/unstable endpoint, which fails about half of the time. The API reports that failure
			// with a status the client retries (a 500, or a 422 that is not no_data_extracted), so most requests succeed in the end
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
