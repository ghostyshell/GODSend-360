// Command dlxb-scrape performs the one-time (or manual) scrape of
// dlxbgame.com and writes the bundled cache file.
//
//	go run ./cmd/dlxb-scrape -out ../../cache/dlxb_games.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"time"

	"godsend/app"
	"godsend/infrastructure/dlxb"
)

func main() {
	out := flag.String("out", "../../cache/dlxb_games.json", "cache file to write")
	api := flag.String("api", app.DlxbAPIURL, "WordPress REST endpoint")
	flag.Parse()

	log.Printf("Scraping %s (this takes a few minutes on first run)", app.DlxbSiteURL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cache, err := dlxb.Scrape(ctx, dlxb.Options{APIURL: *api, Logf: log.Printf})
	if err != nil {
		log.Fatalf("scrape failed: %v", err)
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, data, 0644); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %s: %d ISO + %d Jtag/RGH titles",
		*out, len(cache.Games["iso"]), len(cache.Games["rgh"]))
}
