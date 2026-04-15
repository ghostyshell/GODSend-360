package main

import (
	_ "embed"
	"fmt"
	"time"

	"godsend/services"
)

//go:embed data/iso2god_titles.jsonl
var iso2godTitlesJSONLEmbedded []byte

func init() {
	services.RegisterIso2GodTitlesJSONL(iso2godTitlesJSONLEmbedded)
	services.TitleLookupLog = func(format string, args ...interface{}) {
		fmt.Printf("[%s] "+format+"\n", append([]interface{}{time.Now().Format("15:04:05")}, args...)...)
	}
}
