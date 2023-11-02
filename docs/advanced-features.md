## Advanced Features

### API Mode

`3270Connect` can also run as an API server using the `-api` and `-api-port` flags:

- `-api`: Run `3270Connect` as an API.
- `-api-port`: Specifies the port for the API (default is 8080).

To run `3270Connect` in API mode, use the following command:

```bash
3270Connect -api -api-port 8080
```

Once the API is running, you can send HTTP requests to it to trigger workflows and retrieve information.