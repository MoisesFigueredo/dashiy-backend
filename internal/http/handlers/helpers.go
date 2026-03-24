package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/shopspring/decimal"
)

func decimalToFloat(value decimal.Decimal) float64 {
	amount, _ := value.Round(4).Float64()
	return amount
}

func parseRequiredDate(raw string, field string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fiber.NewError(fiber.StatusBadRequest, field+" is required")
	}

	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, fiber.NewError(fiber.StatusBadRequest, field+" must use YYYY-MM-DD format")
	}
	return parsed, nil
}

func computeRatio(numerator float64, denominator float64) *float64 {
	if denominator == 0 {
		return nil
	}
	value := numerator / denominator
	return &value
}
