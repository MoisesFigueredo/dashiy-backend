package ads

import (
	"errors"
	"fmt"
	"strings"
)

var ErrUnparseableAdName = errors.New("ad name does not match expected convention")

type ParsedName struct {
	Sequence        string `json:"sequence"`
	OfferCode       string `json:"offer_code"`
	LandingCode     string `json:"landing_code"`
	CreativeVariant string `json:"creative_variant"`
	HookCode        string `json:"hook_code"`
	CopyCode        string `json:"copy_code"`
	EditorCode      string `json:"editor_code"`
}

// ParseName parses an ad name into its components.
//
// Accepted formats (7 or 6 segments separated by '_'):
//
//	ADS001_OF09_L04_AD02_H01_COPY01_ED01   (7 segments — full)
//	VAR045_OF09_L04_AD04_H02_EF_MP         (7 segments — VAR prefix)
//	ADS123_OF09_L10_AD001_H11_LF           (6 segments — no editor)
//	VAR010_OF01_L02_AD01_H03_LF            (6 segments — VAR + no editor)
func ParseName(raw string) (*ParsedName, error) {
	name := strings.ToUpper(strings.TrimSpace(stripCampaignPrefix(raw)))
	if name == "" {
		return nil, ErrUnparseableAdName
	}

	parts := strings.Split(name, "_")

	switch len(parts) {
	case 7:
		return parse7(parts)
	case 6:
		return parse6(parts)
	default:
		return nil, ErrUnparseableAdName
	}
}

// parse7 handles the full 7-segment format:
// SEQUENCE_OFFER_LANDING_CREATIVE_HOOK_COPY_EDITOR
func parse7(parts []string) (*ParsedName, error) {
	result := &ParsedName{
		Sequence:        strings.TrimSpace(parts[0]),
		OfferCode:       strings.TrimSpace(parts[1]),
		LandingCode:     strings.TrimSpace(parts[2]),
		CreativeVariant: strings.TrimSpace(parts[3]),
		HookCode:        strings.TrimSpace(parts[4]),
		CopyCode:        strings.TrimSpace(parts[5]),
		EditorCode:      strings.TrimSpace(parts[6]),
	}

	if err := validateSequence(result.Sequence); err != nil {
		return nil, err
	}

	for _, v := range []struct {
		value  string
		prefix string
	}{
		{value: result.OfferCode, prefix: "OF"},
		{value: result.LandingCode, prefix: "L"},
		{value: result.CreativeVariant, prefix: "AD"},
		{value: result.HookCode, prefix: "H"},
	} {
		if err := requirePrefix(v.value, v.prefix); err != nil {
			return nil, err
		}
	}

	if result.CopyCode == "" {
		return nil, ErrUnparseableAdName
	}

	return result, nil
}

// parse6 handles the 6-segment format (no editor code):
// SEQUENCE_OFFER_LANDING_CREATIVE_HOOK_COPY
func parse6(parts []string) (*ParsedName, error) {
	result := &ParsedName{
		Sequence:        strings.TrimSpace(parts[0]),
		OfferCode:       strings.TrimSpace(parts[1]),
		LandingCode:     strings.TrimSpace(parts[2]),
		CreativeVariant: strings.TrimSpace(parts[3]),
		HookCode:        strings.TrimSpace(parts[4]),
		CopyCode:        strings.TrimSpace(parts[5]),
		EditorCode:      "",
	}

	if err := validateSequence(result.Sequence); err != nil {
		return nil, err
	}

	for _, v := range []struct {
		value  string
		prefix string
	}{
		{value: result.OfferCode, prefix: "OF"},
		{value: result.LandingCode, prefix: "L"},
		{value: result.CreativeVariant, prefix: "AD"},
		{value: result.HookCode, prefix: "H"},
	} {
		if err := requirePrefix(v.value, v.prefix); err != nil {
			return nil, err
		}
	}

	if result.CopyCode == "" {
		return nil, ErrUnparseableAdName
	}

	return result, nil
}

// validateSequence accepts ADS* or VAR* as the first segment.
func validateSequence(value string) error {
	if strings.HasPrefix(value, "ADS") || strings.HasPrefix(value, "VAR") {
		return nil
	}
	return fmt.Errorf("%w: sequence must start with ADS or VAR", ErrUnparseableAdName)
}

func stripCampaignPrefix(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return name
	}

	if !strings.Contains(name, "|") {
		return name
	}

	parts := strings.Split(name, "|")
	last := strings.TrimSpace(parts[len(parts)-1])
	if strings.Count(last, "_") >= 5 {
		return last
	}

	return name
}

func requirePrefix(value string, prefix string) error {
	if !strings.HasPrefix(strings.TrimSpace(value), prefix) {
		return fmt.Errorf("%w: missing %s segment", ErrUnparseableAdName, prefix)
	}
	return nil
}
