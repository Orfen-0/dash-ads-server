package utils

import (
	"github.com/Orfen-0/dash-ads-server/internal/database"
	"math"
)

// HaversineDistance returns the distance between two points (in kilometers)
// given their latitude and longitude in decimal degrees.
func HaversineDistance(LocationOne, LocationTwo database.GeoJSONPoint) float64 {
	const R = 6371.0 // Earth radius in kilometers

	// Convert degrees to radians.
	toRad := func(deg float64) float64 {
		return deg * math.Pi / 180
	}

	dLat := toRad(LocationTwo.Coordinates[1] - LocationOne.Coordinates[1])
	dLon := toRad(LocationOne.Coordinates[0] - LocationOne.Coordinates[0])

	lat1Rad := toRad(LocationOne.Coordinates[1])
	lat2Rad := toRad(LocationTwo.Coordinates[1])

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Sin(dLon/2)*math.Sin(dLon/2)*math.Cos(lat1Rad)*math.Cos(lat2Rad)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}
