// Keeps this folder out of the repo's modules: they never build, vet or ship it. The
// harness's Go programs are copied into a fresh module at run time.
module example.com/crawl-acceptance

go 1.25.0
