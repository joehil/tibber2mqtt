# tibber2mqtt
Read data from tibber.com and send them to a home automation server via MQTT.

Once an hour the prices are read and sent to the MQTT server.

The consumption data are read in realtime from the tibber websocket API and sent to the MQTT server. 

The data can be used to automate your home in a way that you get the least possible electricity bill (if you are a tibber.com customer).
