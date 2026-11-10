// Command dlxb-health checks every stored dlxbgame.com link and annotates the
// cache with alive/dead status. Dead links are hidden from the browse lists and
// skipped by the download resolver.
//
//	go run ./cmd/dlxb-health -cache ../../cache/dlxb_games.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"godsend/infrastructure/dlxb"
	"godsend/infrastructure/proxy"
	"godsend/models"
)

func main() {
	cachePath := flag.String("cache", "../../cache/dlxb_games.json", "cache file to update")
	workers := flag.Int("workers", 10, "parallel checks")
	solver := flag.String("solver", "", "optional FlareSolverr-compatible solver URL (unused by the cheap checks)")
	hosts := flag.String("hosts", "", "comma list of hosts to check (default: all)")
	minutes := flag.Int("minutes", 90, "time budget")
	flag.Parse()

	data, err := os.ReadFile(*cachePath)
	if err != nil {
		log.Fatal(err)
	}
	var c models.DlxbCache
	if err := json.Unmarshal(data, &c); err != nil {
		log.Fatal(err)
	}

	r := dlxb.NewResolver()
	r.CFSolverURL = *solver
	if p := proxy.FromEnv(log.Printf); p != nil {
		r.SetProxyPool(p)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*minutes)*time.Minute)
	defer cancel()
	var only map[string]bool
	if strings.TrimSpace(*hosts) != "" {
		only = map[string]bool{}
		for _, h := range strings.Split(*hosts, ",") {
			if h = strings.TrimSpace(h); h != "" {
				only[h] = true
			}
		}
	}
	counts := r.CheckAllFiltered(ctx, &c, *workers, only, log.Printf)

	out, err := json.MarshalIndent(&c, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*cachePath, out, 0644); err != nil {
		log.Fatal(err)
	}

	log.Printf("wrote %s", *cachePath)
	for host, n := range counts {
		log.Printf("  %-12s alive=%d dead=%d unknown=%d", host, n[0], n[1], n[2])
	}
}
