package main

import (
        "log"
        "os"
//      "encoding/json"
        "fmt"
        "io"
	"time"
        "strings"
	"strconv"
        "github.com/spf13/viper"
        "github.com/natefinch/lumberjack"
        "github.com/go-resty/resty/v2"
        "github.com/romshark/jscan"
        mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/tskaard/tibber-golang"
//	"github.com/hasura/go-graphql-client"
//	"nhooyr.io/websocket"
)

var do_trace bool = true

var ownlog string
var tibberurl string
var tibbertoken string
var tibberws string
var tibberhomeid string

var ownlogger io.Writer

var mqttserver string
var mqttport string

var mclient mqtt.Client
var opts = mqtt.NewClientOptions()

var start_time time.Time
var elapsed time.Duration

func main() {
	start_time = time.Now()
	t := time.Now()
	elapsed = t.Sub(start_time)

// Set location of config 
        viper.SetConfigName("tibber2mqtt") // name of config file (without extension)
        viper.AddConfigPath("/etc/")   // path to look for the config file in

// Read config
        read_config()

        opts.AddBroker(fmt.Sprintf("tcp://%s:%s", mqttserver, mqttport))
        opts.SetClientID("tibber2mqtt")
//      opts.SetUsername("emqx")
//      opts.SetPassword("public")
        opts.SetDefaultPublishHandler(messagePubHandler)
        opts.OnConnect = connectHandler
        opts.OnConnectionLost = connectLostHandler
        mclient = mqtt.NewClient(opts)
        if token := mclient.Connect(); token.Wait() && token.Error() != nil {
                panic(token.Error())
        }

// Get commandline args
        if len(os.Args) > 1 {
                a1 := os.Args[1]
                if a1 == "readPrices" {
                        getTibberPrices()
                        os.Exit(0)
                }
                if a1 == "subPower" {
			getTibberSubUrl()
			getTibberHomeId()
                        subTibberPower()
                        os.Exit(0)
                }
                if a1 == "getSubUrl" {
                        getTibberSubUrl()
                        os.Exit(0)
                }
               	if a1 == "getHomeId" {
                        getTibberHomeId()
                        os.Exit(0)
                }
                fmt.Println("parameter invalid")
                os.Exit(-1)
        }
        if len(os.Args) == 1 {
                myUsage()
        }
}

func read_config() {
        err := viper.ReadInConfig() // Find and read the config file
        if err != nil { // Handle errors reading the config file
                log.Fatalf("Config file not found: %v", err)
        }

        ownlog = viper.GetString("own_log")
        if ownlog =="" { // Handle errors reading the config file
                log.Fatalf("Filename for ownlog unknown: %v", err)
        }
// Open log file
        ownlogger = &lumberjack.Logger{
                Filename:   ownlog,
                MaxSize:    5, // megabytes
                MaxBackups: 3,
                MaxAge:     28, //days
                Compress:   true, // disabled by default
        }
//        defer ownlogger.Close()
        log.SetOutput(ownlogger)

        tibberurl = viper.GetString("tibberurl")
        tibbertoken = viper.GetString("tibbertoken")
        mqttserver = viper.GetString("mqttserver")
        mqttport = viper.GetString("mqttport")

        if do_trace {
                log.Println("do_trace: ",do_trace)
                log.Println("own_log; ",ownlog)
        }
}

var messagePubHandler mqtt.MessageHandler = func(client mqtt.Client, msg mqtt.Message) {
    fmt.Printf("Received message: %s from topic: %s\n", msg.Payload(), msg.Topic())
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
    fmt.Printf("Connected to MQTT server %s\n",mqttserver)
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
    fmt.Printf("Connect lost: %v", err)
}

func myUsage() {
     fmt.Printf("Usage: %s argument\n", os.Args[0])
     fmt.Println("Arguments:")
     fmt.Println("readPrices    Read prices for today and tomorrow (only available after 1pm)")
     fmt.Println("subPower      Subscribe to webservice to get current power consumption")
     fmt.Println("getSubUrl	Get Url to use for subscriptions")
     fmt.Println("getHomeId     Get ID of active home")
}

func SortASC(a []float64) []float64 {
	for i := 0; i < len(a)-1; i++ {
		for j := i + 1; j < len(a); j++ {
			if a[i] >= a[j] {
				temp := a[i]
				a[i] = a[j]
				a[j] = temp
			}
		}
	}
	return a
}

func getTibberPrices() {
        var tibberquery string = `{ "query": "{viewer {homes {currentSubscription {priceInfo {current {total startsAt} today {total startsAt} tomorrow {total startsAt}}}}}}"}`
        var total string
	var ftotal float64 = 0
	var ftomorrow float64 = 0
	var topic string = "topic/out/"
	var temp string
	var ctotal int8 = 0
	var ctomorrow int8 = 0
	var mintotal float64 = 99
	var maxtotal float64 = 0
	var diff float64 = 0
	var m1 float64 = 0
	var m2 float64 = 0

	var prices []float64

        token := mclient.Publish("topic/out/state", 0, false, "on")
        token.Wait()

        // Create a Resty Client
        client := resty.New()

        // POST JSON string
        // No need to set content type, if you have client level setting
        resp, err := client.R().
                SetHeader("Content-Type", "application/json").
                SetBody(tibberquery).
                SetAuthToken(tibbertoken).
                Post(tibberurl)
        if err == nil {
                err = jscan.Scan(jscan.Options{
                        CachePath:  true,
                        EscapePath: true,
                }, string(resp.Body()), func(i *jscan.Iterator) (err bool) {
                        if i.Key() == "total" {
                                total = i.Value();
                        }
                        if i.Key() == "startsAt" {
                                temp = topic + "total" + i.Value()[11:13]
                                if strings.Contains(i.Path(), "tomorrow"){
                                        temp = topic + "tomorrow" + i.Value()[11:13]
                                        val,_ := strconv.ParseFloat(total,64)
                                        ftomorrow += val
					ctomorrow++ 
                                } else {
					val,_ := strconv.ParseFloat(total,64)
					ftotal += val
					ctotal++ 
					if val < mintotal {
						mintotal = val
					}
                                        if val > maxtotal {
                                                maxtotal = val
                                        }
					prices = append(prices,val)
				}
                                token = mclient.Publish(temp, 0, false, total)
                                token.Wait()
                        }
                        return false // No Error, resume scanning
                })
                temp = topic + "totalmean"
                token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",ftotal/float64(ctotal)))
                token.Wait()
                temp = topic + "tomorrowmean"
                token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",ftomorrow/float64(ctomorrow)))
                token.Wait()
		diff = maxtotal - mintotal
		diff = diff / 3
		m1 = mintotal + diff
		m2 = m1 + diff
                mintotal += float64(0.01)
                temp = topic + "mintotal"
                token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",mintotal))
                token.Wait()
                temp = topic + "maxtotal"
                token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",maxtotal))
                token.Wait()
                temp = topic + "m1"
                token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",m1))
                token.Wait()
                temp = topic + "m2"
                token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",m2))
                token.Wait()

		prices = SortASC(prices)
		for i := 0; i < len(prices)-1; i++ {
                	temp = topic + "t" + fmt.Sprintf("%d",i)
                	token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",prices[i]))
                	token.Wait()
		}
        } else {
                fmt.Println(err)
        }
}

func getTibberSubUrl(){
        var tibberquery string = `{ "query": "{viewer {websocketSubscriptionUrl } }"}`
        // Create a Resty Client
        client := resty.New()

        // POST JSON string
        // No need to set content type, if you have client level setting
        resp, err := client.R().
                SetHeader("Content-Type", "application/json").
                SetBody(tibberquery).
                SetAuthToken(tibbertoken).
                Post(tibberurl)
        if err == nil {
                err = jscan.Scan(jscan.Options{
                        CachePath:  true,
                        EscapePath: true,
                }, string(resp.Body()), func(i *jscan.Iterator) (err bool) {
                        if i.Key() == "websocketSubscriptionUrl" {
                                tibberws = i.Value()
				fmt.Println(tibberws)
                        }
			return false
                })
        } else {
                fmt.Println(err)
        }
}

func getTibberHomeId(){
	var homeid string
        var tibberquery string = `{ "query": "{viewer {homes {id features {realTimeConsumptionEnabled } } } }"}`
        // Create a Resty Client
        client := resty.New()

        // POST JSON string
        // No need to set content type, if you have client level setting
        resp, err := client.R().
                SetHeader("Content-Type", "application/json").
                SetBody(tibberquery).
                SetAuthToken(tibbertoken).
                Post(tibberurl)
        if err == nil {
                err = jscan.Scan(jscan.Options{
                        CachePath:  true,
                        EscapePath: true,
                }, string(resp.Body()), func(i *jscan.Iterator) (err bool) {
                        if i.Key() == "id" {
                                homeid = i.Value()
                        }
                        if i.Key() == "realTimeConsumptionEnabled" {
				if i.Value() == "true" {
                                	tibberhomeid = homeid
                                }
                        }
                        return false
                })
        } else {
                fmt.Println(err)
        }
	fmt.Println(tibberhomeid)
}

type Handler struct {
        tibber     *tibber.Client
        streams    map[string]*tibber.Stream
        msgChannal tibber.MsgChan
}

func NewHandler() *Handler {
        h := &Handler{}
        h.tibber = tibber.NewClient("")
        h.streams = make(map[string]*tibber.Stream)
        h.msgChannal = make(tibber.MsgChan)
        return h
}

func subTibberPower(){
    h := NewHandler()
        h.tibber.Token = tibbertoken
        homes, err := h.tibber.GetHomes()
        if err != nil {
                panic("Can not get homes from Tibber")
        }
        for _, home := range homes {
                fmt.Println(home.ID)
                if home.Features.RealTimeConsumptionEnabled {
                        stream := tibber.NewStream(home.ID, h.tibber.Token)
                        stream.StartSubscription(h.msgChannal)
                        h.streams[home.ID] = stream
                }
        }
        _, err = h.tibber.SendPushNotification("Tibber-Golang", "Message from GO")
        if err != nil {
                panic("Push failed")
        }
        go func(msgChan tibber.MsgChan) {
                for {
                        select {
                        case msg := <-msgChan:
                                h.handleStreams(msg)
                        }
                }
        }(h.msgChannal)

        for {
		time.Sleep(10*time.Second)
        }

}

func (h *Handler) handleStreams(newMsg *tibber.StreamMsg) {
	t := time.Now()
	elapsed = t.Sub(start_time)
	if elapsed > 60000000000 {
        	token := mclient.Publish("topic/out/livePower", 0, false, fmt.Sprintf("%.0f",newMsg.Payload.Data.LiveMeasurement.Power))
        	token.Wait()
                token = mclient.Publish("topic/out/accPower", 0, false, fmt.Sprintf("%.3f",newMsg.Payload.Data.LiveMeasurement.AccumulatedConsumption))
                token.Wait()
                token = mclient.Publish("topic/out/accCost", 0, false, fmt.Sprintf("%.2f",newMsg.Payload.Data.LiveMeasurement.AccumulatedCost))
                token.Wait()
		start_time = time.Now()
	}
}


