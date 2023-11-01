## Advanced Features

### API Mode

`go3270` can also run as an API server using the `-api` and `-api-port` flags:

- `-api`: Run `go3270` as an API.
- `-api-port`: Specifies the port for the API (default is 8080).

To run `go3270` in API mode, use the following command:

```bash
go3270 -api -api-port 8080
```

Once the API is running, you can send HTTP requests to it to trigger workflows and retrieve information.