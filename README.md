# HTTP Adapter


### What Is This?

This is a custom [Logspout](https://github.com/gliderlabs/logspout) build that adds and HTTP adapter so Logspout can forward to an HTTP(S) endpoint that supports receiving logs via POST. This build does not include the raw or syslog adapters, it is meant to be used to forward to HTTP endpoints and nothing else.


### Using The Image From Docker Hub

This again assumes that the unique token for the Sumo Logic HTTP collector endpoint is in the environment as `$SUMO_HTTP_TOKEN`.

```bash
$ docker run -e DEBUG=1 \
    -v /var/run/docker.sock:/tmp/docker.sock \
    -e LOGSPOUT=ignore \
    raychaser/logspout-http:latest \
    https://collectors.sumologic.com?http.path=/receiver/v1/http/$SUMO_HTTP_TOKEN\&http.gzip=true
```


### A Note On The Form Of The Route Parameter

The route URI for an HTTP(S) endpoint should just include the hostname. The HTTP path currently has to specified as a parameter. For example, for Sumo Logic the endpoint URI with for an HTTP collector endpoint would look like this:

```
https://collectors.sumologic.com/receiver/v1/http/SUMO_HTTP_TOKEN
```

But for Logspout, it needs to be written like this:

```
https://collectors.sumologic.com?http.path=/receiver/v1/http/SUMO_HTTP_TOKEN
```


### Additional Parameters

In addition to the `http.path` parameter discussed above, the following parameters are available:

`http.buffer.capacity` controls the size of a buffer used to accumulate logs. The default capacity of the buffer is 100 logs.

`http.buffer.timeout` indicates after how much time the adapter will send the logs accumulated in the buffer if the buffer capacity hasn't been reached. The default timeout is 1000ms (1s).

If `http.gzip` is set to true, the logs will be compressed with GZIP. This is off by default, but for example supported by Sumo Logic.

Override docker hostname with `hostname` parameter

### Basic Http Authentication
Use `http.user` and `http.password` parameters to set Authorization header



### Development 

This assumes that the unique token for the Sumo Logic HTTP collector endpoint is in the environment as ```$SUMO_HTTP_TOKEN```.

```bash
$ DEBUG=1 ROUTE=https://collectors.sumologic.com?http.buffer.timeout=1s\&http.buffer.capacity=100\&http.path=/receiver/v1/http/$SUMO_HTTP_TOKEN\&http.gzip=true make dev
```

To create some test messages

```bash
$ docker run --rm --name test ubuntu bash -c 'NEW_UUID=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1); for i in `seq 1 10`; do echo $NEW_UUID Hello $i; sleep 1; done' && CID=$(docker ps -l -q)
```

If you are a fan of Loggly, use this route (`$LOGGLY_HTTP_TOKEN` is your Loggly HTTP input token):

```bash
$ ROUTE=http://logs-01.loggly.com?http.path=/bulk/$LOGGLY_HTTP_TOKEN/tag/bulk/ make dev
```

Loggly does not support GZIP compression. It does however support HTTPS, so this will also work:

```bash
$ ROUTE=https://logs-01.loggly.com?http.path=/bulk/$LOGGLY_HTTP_TOKEN/tag/bulk/ make dev
```


### Todos

- [ ] Deal with errors and non-200 responses... somehow
- [ ] Make sure we send back the AWS ELB cookie if we get one
- [X] Add compression option


### Issues Found While Writing This Adapter

Log of stuff I ran into to verify, validate, discuss, fix, ...

* Cannot figure out how to create a "custom" build from my own source code
* Full URL is not passed as part of Route, so Sumo-style endpoint URL where the path is relevant and includes auth info isn't working
* Uninitialized options map when using non-standard parameter (#75, #76)
* Logspout seems to need ```-e LOGSPOUT=ignore``` to prevent getting into a feedback loop when using debug output - need to validate this feedback loop is a universal possibility or just something i backed myself into
* Makefile needs quotes around ```$(ROUTE)``` if URL includes ampersand, and ampersand needs to be quoted
```bash
$ DEBUG=1 \
    ROUTE=https://collectors.sumologic.com?http.buffer.timeout=30s\& make dev
```
* Docker 1.6 with ```--log-driver=none``` or ```--log-driver=syslog``` will break Logspout


### Issue With --log-driver In Docker 1.6

Just writing this down here for now so I don't lose it... Likely should be discussed in the Docker context, not the Logspout context. Logspout will not see any container output if the ```--log-driver``` (new in Docker 1.6) is set to anything but the default (```json-file```). Proof:

```bash
$ docker run --rm -it --name test1 ubuntu bash -c 'NEW_UUID=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1); for i in `seq 1 10000`; do echo $NEW_UUID Hello $i; sleep 1; done'
$ CID=$(docker ps -l -q)
$ echo -e "GET /containers/c4074eb48952/logs?stdout=1 HTTP/1.0\r\n" | nc -U /var/run/docker.sock
```

This should return the the logs of the started container.

```bash
$ docker stop $CID
$ docker run --rm -it --log-driver=syslog --name test1 ubuntu bash -c 'NEW_UUID=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1); for i in `seq 1 10000`; do echo $NEW_UUID Hello $i; sleep 1; done'
$ CID=$(docker ps -l -q)
$ echo -e "GET /containers/c4074eb48952/logs?stdout=1 HTTP/1.0\r\n" | nc -U /var/run/docker.sock
```

Will return:

```
"logs" endpoint is supported only for "json-file" logging driver
Error running logs job: "logs" endpoint is supported only for "json-file" logging driver
```

This is unfortunate because it prevents using --log-driver=none in conjunction with Logspout to forward logs without touching the host disk. With ```json-file``` being required, the issue of logs running the host of disk space still remains.


### Dev Notes And Command Line Snippets

```
git add http/ && git commit -m "Debugging" && git push && touch modules.go && docker build -t logspout-http .


ROUTE=https://collectors.sumologic.com?http.buffer.timeout=2s\&http.buffer.capacity=10\&http.path=/receiver/v1/http/$SUMO_HTTP_TOKEN make dev

docker run --rm --name test ubuntu bash -c 'NEW_UUID=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1); for i in `seq 1 66`; do echo $NEW_UUID Hello $i; sleep .5; done'


docker run --rm --name logspout-http -e DEBUG=true -e LOGSPOUT=ignore -v /var/run/docker.sock:/tmp/docker.sock logspout-http https://collectors.sumologic.com?http.path=/receiver/v1/http/$SUMO_HTTP_TOKEN


docker run --rm --name test1 ubuntu bash -c 'NEW_UUID=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1); for i in `seq 1 1000000`; do echo $NEW_UUID Hello $i; sleep .001; done'

docker run --rm --name test2 ubuntu bash -c 'NEW_UUID=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1); for i in `seq 1 1000000`; do echo $NEW_UUID Hello $i; sleep .001; done'

docker run --rm -i --name test3 ubuntu bash -c 'NEW_UUID=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1); for i in `seq 1 1000000`; do echo $NEW_UUID Hello $i; sleep .001; done'

docker run --rm -i --name test4 ubuntu bash -c 'NEW_UUID=$(cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1); for i in `seq 1 1000000`; do echo $NEW_UUID Hello $i; sleep .001; done'
```

