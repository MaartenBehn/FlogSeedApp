package main

import (
	"fmt"
	"github.com/go-echarts/go-echarts/v2/opts"
	"net/http"
	"strconv"
	"sync"
	"time"
	"tinygo.org/x/bluetooth"
)

const verbose = false

const FLOGSEEDMAC = "35:3C:F9:39:E3:4D"
const SPEEDUUID = "19B10001-E8F2-537E-4F6C-D104768A1214"

var adapter = bluetooth.DefaultAdapter

type device struct {
	result       bluetooth.ScanResult
	lastTimeSeen time.Time
}

var deviceList = map[string]device{}
var newDevices = make(chan bluetooth.ScanResult, 10)
var disconnectDevice = make(chan int)
var newSpeedData = make(chan []uint8)

var mutex sync.Mutex

func httpserver(w http.ResponseWriter, _ *http.Request) {

}

var items = make([]opts.LineData, 0)
var speedData = make([]uint8, 0)

func main() {
	must("enable BLE stack", adapter.Enable())

	go scanForDevices()

	go monitorPossibleDevices()

	go monitorSpeed()

	http.HandleFunc("/", httpserver)
	http.HandleFunc("/speed", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, speedData)
	})

	err := http.ListenAndServe(":8081", nil)
	must("webserver", err)
}

func scanForDevices() {
	println("scanning...")
	err := adapter.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		newDevices <- result

	})
	must("start scan", err)
}

func monitorPossibleDevices() {
	lastCleanTime := time.Now()
	for result := range newDevices {
		addDevice(result)

		if time.Since(lastCleanTime).Seconds() >= 5 {
			lastCleanTime = time.Now()

			println("Scan Results:")
			for _, device := range deviceList {
				if time.Since(device.lastTimeSeen).Seconds() >= 5 {
					delete(deviceList, device.result.Address.String())
				}

				println("device:", device.result.Address.String(), device.result.RSSI, device.result.LocalName())
			}
		}

		if result.Address.String() == FLOGSEEDMAC {
			connect(result)
		}
	}
}

func addDevice(result bluetooth.ScanResult) {
	addr := result.Address.String()
	deviceList[addr] = device{
		result:       result,
		lastTimeSeen: time.Now(),
	}
}

func connect(result bluetooth.ScanResult) {
	device, err := adapter.Connect(result.Address, bluetooth.ConnectionParams{})
	must("connect", err)

	println("connected to ", result.Address.String())

	speedUUID, err := bluetooth.ParseUUID(SPEEDUUID)
	must("parse UUID", err)

	println("discovering services/characteristics")
	srvcs, err := device.DiscoverServices(nil)
	must("discover services", err)

	buf := make([]byte, 255)

	for _, srvc := range srvcs {
		println("- service", srvc.UUID().String())

		chars, err := srvc.DiscoverCharacteristics(nil)
		if err != nil {
			println(err)
		}
		for _, char := range chars {
			println("-- characteristic", char.UUID().String())
			n, err := char.Read(buf)
			if err != nil {
				println("    ", err.Error())
			} else {
				println("    data bytes", strconv.Itoa(n))
				println("    value =", string(buf[:n]))
			}

			if char.UUID() == speedUUID {
				err := char.EnableNotifications(func(buf []byte) {
					newSpeedData <- buf
				})
				if err != nil {
					println(err)
				}
			}
		}
	}

	<-disconnectDevice

	err = device.Disconnect()
	must("disconnect", err)
}

func monitorSpeed() {
	for buf := range newSpeedData {

		mutex.Lock()
		println("data:", uint8(buf[0]))
		items = append(items, opts.LineData{Value: uint8(buf[0])})
		speedData = append(speedData, buf[0])

		if len(items) > 100 {
			items = items[len(items)-100:]
		}
		if len(speedData) > 100 {
			speedData = speedData[len(speedData)-100:]
		}
		mutex.Unlock()
	}
}

func must(action string, err error) {
	if err != nil {
		panic("failed to " + action + ": " + err.Error())
	}
}
