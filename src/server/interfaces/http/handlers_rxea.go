// handlers_rxea.go — RXEA asset encode/decode HTTP handlers.
package http

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	stdhttp "net/http"
	"strconv"

	"godsend/utils"
)

// handleRXEADecode decodes an Aurora RXEA asset file → PNG(s).
//
// POST /rxea/decode
// Body: raw RXEA bytes (multipart or raw, Content-Type: application/octet-stream)
// Query: ?slot=0..24  (optional — when absent, returns all non-empty slots)
//
// Response JSON:
//
//	{ "slots": [ { "slot": 4, "width": 1024, "height": 960, "png": "<base64>" }, ... ] }
func (d *Deps) handleRXEADecode(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		stdhttp.Error(w, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}

	rxeaBytes, err := io.ReadAll(r.Body)
	if err != nil || len(rxeaBytes) == 0 {
		jsonError(w, stdhttp.StatusBadRequest, "empty body")
		return
	}

	slotStr := r.URL.Query().Get("slot")

	type slotResult struct {
		Slot   int    `json:"slot"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		PNG    []byte `json:"png"` // raw bytes; will be base64 by json.Marshal
	}

	var results []slotResult

	if slotStr != "" {
		slotIdx, perr := strconv.Atoi(slotStr)
		if perr != nil || slotIdx < 0 || slotIdx >= 25 {
			jsonError(w, stdhttp.StatusBadRequest, "invalid slot")
			return
		}
		img, derr := utils.DecodeRXEASlot(rxeaBytes, utils.AssetSlot(slotIdx))
		if derr != nil {
			jsonError(w, stdhttp.StatusUnprocessableEntity, derr.Error())
			return
		}
		if img == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"slots": []slotResult{}})
			return
		}
		var buf bytes.Buffer
		if err := pngEnc.Encode(&buf, img); err != nil {
			jsonError(w, stdhttp.StatusInternalServerError, err.Error())
			return
		}
		results = append(results, slotResult{slotIdx, img.Bounds().Dx(), img.Bounds().Dy(), buf.Bytes()})
	} else {
		entries, diags, derr := utils.DecodeRXEA(rxeaBytes)
		if derr != nil {
			jsonError(w, stdhttp.StatusUnprocessableEntity, derr.Error())
			return
		}
		for _, e := range entries {
			var buf bytes.Buffer
			if err := pngEnc.Encode(&buf, e.Img); err != nil {
				continue
			}
			results = append(results, slotResult{int(e.Slot), e.Width, e.Height, buf.Bytes()})
		}
		// Include diagnostics so callers can see per-slot format info and errors.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"slots": results, "diags": diags})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"slots": results})
}

// handleRXEAEncode encodes an image (PNG, JPEG, etc.) into an RXEA asset file.
//
// POST /rxea/encode
// Body: raw image bytes (PNG, JPEG, or any Go-supported format)
// Query: ?slot=0..24  (required — which asset slot this image occupies)
//
// Response: raw RXEA bytes (Content-Type: application/octet-stream)
func (d *Deps) handleRXEAEncode(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		stdhttp.Error(w, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}

	slotStr := r.URL.Query().Get("slot")
	if slotStr == "" {
		jsonError(w, stdhttp.StatusBadRequest, "slot query param required")
		return
	}
	slotIdx, perr := strconv.Atoi(slotStr)
	if perr != nil || slotIdx < 0 || slotIdx >= 25 {
		jsonError(w, stdhttp.StatusBadRequest, "invalid slot (0–24)")
		return
	}

	imgBytes, err := io.ReadAll(r.Body)
	if err != nil || len(imgBytes) == 0 {
		jsonError(w, stdhttp.StatusBadRequest, "empty body")
		return
	}

	img, _, err := image.Decode(bytes.NewReader(imgBytes))
	if err != nil {
		jsonError(w, stdhttp.StatusUnprocessableEntity, "unsupported image format: "+err.Error())
		return
	}

	rxeaBytes, err := utils.EncodeRXEA(utils.AssetSlot(slotIdx), img)
	if err != nil {
		jsonError(w, stdhttp.StatusUnprocessableEntity, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(rxeaBytes)))
	w.Write(rxeaBytes) //nolint:errcheck
}

// handleRXEAEncodeMulti encodes multiple images into a single RXEA file.
//
// POST /rxea/encode-multi
// Body: JSON { "slots": [{ "slot": 0, "png": "base64" }, ...] }
// Response: raw RXEA bytes (Content-Type: application/octet-stream)
func (d *Deps) handleRXEAEncodeMulti(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodPost {
		stdhttp.Error(w, "method not allowed", stdhttp.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Slots []struct {
			Slot int    `json:"slot"`
			PNG  string `json:"png"`
		} `json:"slots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, stdhttp.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(req.Slots) == 0 {
		jsonError(w, stdhttp.StatusBadRequest, "no slots")
		return
	}

	slotImages := make([]utils.SlotImage, 0, len(req.Slots))
	for _, s := range req.Slots {
		if s.Slot < 0 || s.Slot >= 25 {
			jsonError(w, stdhttp.StatusBadRequest, "invalid slot (0–24)")
			return
		}
		imgBytes, err := base64.StdEncoding.DecodeString(s.PNG)
		if err != nil {
			jsonError(w, stdhttp.StatusBadRequest, "bad base64 for slot "+strconv.Itoa(s.Slot))
			return
		}
		img, _, err := image.Decode(bytes.NewReader(imgBytes))
		if err != nil {
			jsonError(w, stdhttp.StatusUnprocessableEntity, "unsupported image for slot "+strconv.Itoa(s.Slot)+": "+err.Error())
			return
		}
		slotImages = append(slotImages, utils.SlotImage{
			Slot: utils.AssetSlot(s.Slot),
			Img:  img,
		})
	}

	rxeaBytes, err := utils.EncodeRXEAMulti(slotImages)
	if err != nil {
		jsonError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(rxeaBytes)))
	w.Write(rxeaBytes) //nolint:errcheck
}

// pngEnc is the default PNG encoder (no compression options needed).
var pngEnc = &png.Encoder{CompressionLevel: png.BestSpeed}
