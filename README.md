Build and run. This will create a quick tunnel that http://localhost:8080 will be accessible at. On restart it will try to reuse the prior tunnel if possible, if a new tunnel needs to be created, the server will be notified with a post to http://localhost:8080/callback

```
go build ./cmd/cloudflared-quick-tunnel
./cloudflared-quick-tunnel run --url http://localhost:8080 --callback callback
```

Run a test server. We will see the tunnel url printed out and we can make /ping requests to it.

```
go build ./cmd/test-server
./test-server
```