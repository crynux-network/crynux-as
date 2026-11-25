package service

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"testing"
)

func testPNGDataURL(t *testing.T, width, height int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestVideoTaskUnitsDefaults(t *testing.T) {
	units, err := videoTaskUnits(`{"prompt":"a cat"}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := uint64(832 * 480 * 81 * 30); units != want {
		t.Fatalf("units = %d, want %d", units, want)
	}
}

func TestVideoTaskUnitsExplicitConfig(t *testing.T) {
	units, err := videoTaskUnits(`{"task_config":{"width":640,"height":360,"num_frames":49,"steps":20}}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := uint64(640 * 360 * 49 * 20); units != want {
		t.Fatalf("units = %d, want %d", units, want)
	}
}

func TestVideoTaskUnitsUsesConditionImageDimensions(t *testing.T) {
	img := testPNGDataURL(t, 64, 32)
	cases := []string{
		`{"image":"` + img + `","task_config":{"num_frames":10,"steps":2}}`,
		`{"last_image":"` + img + `","task_config":{"num_frames":10,"steps":2}}`,
		`{"keyframes":[{"image":"` + img + `"}],"task_config":{"num_frames":10,"steps":2}}`,
		`{"references":[{"type":"video","data":"x"},{"type":"image","data":"` + img + `"}],"task_config":{"num_frames":10,"steps":2}}`,
	}
	for i, taskArgs := range cases {
		units, err := videoTaskUnits(taskArgs)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if units != 64*32*10*2 {
			t.Fatalf("case %d: units = %d", i, units)
		}
	}

	units, err := videoTaskUnits(`{"image":"` + img + `","task_config":{"width":100,"num_frames":10,"steps":2}}`)
	if err != nil {
		t.Fatal(err)
	}
	if units != 100*32*10*2 {
		t.Fatalf("partial override units = %d", units)
	}
}

func TestVideoTaskUnitsRejectsInvalidInput(t *testing.T) {
	cases := []string{
		`{"image":"https://example.com/a.png"}`,
		`{"image":"data:image/png;base64,not-base64"}`,
		`{"task_config":{"num_frames":0}}`,
		`{"task_config":{"width":18446744073709551615,"height":2,"num_frames":1,"steps":1}}`,
	}
	for i, taskArgs := range cases {
		if _, err := videoTaskUnits(taskArgs); err == nil {
			t.Fatalf("case %d accepted", i)
		}
	}
}
