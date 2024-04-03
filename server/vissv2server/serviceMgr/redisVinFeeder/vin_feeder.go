// !!!!! redisInit must be executed before running feederclient !!!!

package main

import (
	"fmt"
	"github.com/covesa/vissr/utils"
	"github.com/go-redis/redis"
	"os"
	"time"
)

type RedisDp struct {
	Val string
	Ts  string
}

var feederClient *redis.Client

func redisSet(client *redis.Client, path string, val string, ts string) int {
	dp := `{"val":"` + val + `", "ts":"` + ts + `"}`
	// ctx := context.TODO()
	err := client.Set(path, dp, time.Duration(0)).Err()
	if err != nil {
		fmt.Printf("Job failed. Err=%s\n", err)
		return -1
	} else {
		fmt.Println("Datapoint=%s\n", dp)
		return 0
	}
}

func main() {
	var vehicleVin string
	if len(os.Args) != 2 {
		fmt.Printf("VIN feeder command line: ./vin_feeder VIN\n")
		os.Exit(1)
	}
	vehicleVin = os.Args[1]

	feederClient = redis.NewClient(&redis.Options{
		Network:  "unix",
		Addr:     utils.GetUdsPath("Vehicle", "redis"),
		Password: "",
		DB:       1,
	})

	cPath := "Vehicle.VehicleIdentification.VIN"
	fmt.Printf("Path to current datapoint=%s\n", cPath)

	Cvalue := vehicleVin
	Cts := "2022-02-21T13:37:00Z"
	fmt.Printf("Current value=%s, current timestamp=%s\n", Cvalue, Cts)

	status := redisSet(feederClient, cPath, Cvalue, Cts)
	if status != 0 {
		fmt.Printf("Feeder-redisSet() call failed.\n")
	} else {
		fmt.Printf("Feeder-redisSet() call succeeded.\n")
	}
}
