package main

import (
        "log"
        "os"
        "encoding/json"
        "fmt"
        "io"
	"net/http"
	"time"
        "strings"
	"strconv"
        "github.com/spf13/viper"
        "github.com/natefinch/lumberjack"
        "github.com/go-resty/resty/v2"
        "github.com/romshark/jscan"
        mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/hasura/go-graphql-client"
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

type headerRoundTripper struct {
	setHeaders func(req *http.Request)
	rt         http.RoundTripper
}

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
        b := make([]float64, len(a))
        copy(b, a)
	for i := 0; i < len(b)-1; i++ {
		for j := i + 1; j < len(b); j++ {
			if b[i] >= b[j] {
				temp := b[i]
				b[i] = b[j]
				b[j] = temp
			}
		}
	}
	return b
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
		if mintotal > float64(1) {
			mintotal = float64(0.2)
		}
                if m1 > float64(1) {
                        m1 = float64(0.2)
                }
                if m2 > float64(1) {
                        m2 = float64(0.3)
                }
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

		pricest := SortASC(prices)
		for i := 1; i < len(pricest); i++ {
                	temp = topic + "t" + fmt.Sprintf("%d",i)
                	token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",pricest[i-1]))
                	token.Wait()
		}

                pricesn := SortASC(prices[0:6])
                for i := 1; i < 7; i++ {
                        temp = topic + "n" + fmt.Sprintf("%d",i)
                        token = mclient.Publish(temp, 0, false, fmt.Sprintf("%.4f",pricesn[i-1]))
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

func subTibberPower() error {
	// get the demo token from the graphiql playground
	demoToken := tibbertoken
	if demoToken == "" {
		panic("Token is required")
	}

	client := graphql.NewSubscriptionClient(tibberws).
		WithProtocol(graphql.GraphQLWS).
		WithWebSocketOptions(graphql.WebsocketOptions{
			HTTPClient: &http.Client{
				Transport: headerRoundTripper{
					setHeaders: func(req *http.Request) {
						req.Header.Set("User-Agent", "go-graphql-client/0.9.0")
					},
					rt: http.DefaultTransport,
				},
			},
		}).
		WithConnectionParams(map[string]interface{}{
			"token": demoToken,
		}).WithLog(log.Println).
		OnError(func(sc *graphql.SubscriptionClient, err error) error {
			panic(err)
		})

	defer client.Close()

	var sub struct {
		LiveMeasurement struct {
			Power                  int       `graphql:"power"`
                        PowerProduction        int       `graphql:"powerProduction"`
			AccumulatedConsumption float64   `graphql:"accumulatedConsumption"`
			AccumulatedCost        float64   `graphql:"accumulatedCost"`
		} `graphql:"liveMeasurement(homeId: $homeId)"`
	}

	variables := map[string]interface{}{
		"homeId": graphql.ID(tibberhomeid),
	}
	_, err := client.Subscribe(sub, variables, func(data []byte, err error) error {

		if err != nil {
			fmt.Println("ERROR: ", err)
			return nil
		}

		if data == nil {
			return nil
		}

                fmt.Printf("%s :: %s\n",time.Now().Format(time.RFC850),string(data))

                var tLive map[string]interface{}
                var power float64
                var powerProd float64
                var accCons float64
                var accCost float64

                err = json.Unmarshal([]byte(data), &tLive)
	        if err != nil {
		        fmt.Printf("could not unmarshal json: %s\n", err)
		        return nil
	        }

                measure := tLive["liveMeasurement"].(map[string]interface{})

                for key, value := range measure {
                        // Each value is an `any` type, that is type asserted as a string
                        if key == "power" {
                           power = value.(float64)
                        }
                        if key == "powerProduction" {
                           powerProd = value.(float64)
                        }
                        if key == "accumulatedConsumption" {
                           accCons = value.(float64)
                        }
                        if key == "accumulatedCost" {
                           accCost = value.(float64)
                        }
                }

                var powerGes float64 = power - powerProd

                token := mclient.Publish("topic/out/", 0, false, fmt.Sprintf("%v",string(data)))
                token.Wait() 
                token = mclient.Publish("topic/out/powerGes", 0, false, fmt.Sprintf("%0.0f",powerGes))
                token.Wait()

                var priceAvg float64 = accCost / accCons

                token = mclient.Publish("topic/out/priceAvg", 0, false, fmt.Sprintf("%0.4f",priceAvg))
                token.Wait()

		return nil
	})

	if err != nil {
		panic(err)
	}

	return client.Run()
}

func (h headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	h.setHeaders(req)
	return h.rt.RoundTrip(req)
}

