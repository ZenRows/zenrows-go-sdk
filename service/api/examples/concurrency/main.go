package main

import (
	"context"
	"fmt"
	"sync"

	scraperapi "github.com/zenrows/zenrows-go-sdk/service/api"
)

const (
	maxConcurrentRequests = 5  // run 5 scraping requests at the same time
	totalRequests         = 15 // send a total of 15 scraping requests
)

func main() {
	client := scraperapi.NewClient(
		scraperapi.WithAPIKey("YOUR_API_KEY"),
		scraperapi.WithMaxConcurrentRequests(maxConcurrentRequests),
	)

	var wg sync.WaitGroup
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			res, err := client.Get(context.Background(), "https://httpbin.io/anything", &scraperapi.RequestParameters{})
			if err != nil {
				fmt.Println(i, err)
				return
			}

			if err = res.Error(); err != nil {
				fmt.Println(i, err)
				return
			}

			fmt.Printf("[#%d]: %s\n", i, res.Status())
		}(i)
	}

	wg.Wait()
	fmt.Println("done")
}
