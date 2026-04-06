package main

import (
	_ "embed"

	"godsend/services"
)

//go:embed data/iso2god_titles.jsonl
var iso2godTitlesJSONLEmbedded []byte

func init() {
	services.RegisterIso2GodTitlesJSONL(iso2godTitlesJSONLEmbedded)
	services.TitleLookupLog = logf
}
