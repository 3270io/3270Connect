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

POST:

```bash
http://localhost:8080/api/execute
```

Body:
```json
{
  "Host": "10.27.27.27",
  "Port": 3270,
  "HTMLFilePath": "output.html",
  "Steps": [
    {
      "Type": "InitializeHTMLFile"
    },
    {
      "Type": "Connect"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "CheckValue",
      "Coordinates": {"Row": 1, "Column": 2, "Length": 11},
      "Text": "Some: VALUE"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 10, "Column": 44},
      "Text": "user1"
    },
    {
      "Type": "FillString",
      "Coordinates": {"Row": 11, "Column": 44},
      "Text": "mypass"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "PressEnter"
    },
    {
      "Type": "AsciiScreenGrab"
    },
    {
      "Type": "Disconnect"
    }
  ]
}
```

### API Mode with Docker

`3270Connect` can also run as an API server using the `-api` and `-api-port` flags:

- `-api`: Run `3270Connect` as an API.
- `-api-port`: Specifies the port for the API (default is 8080).

To run `3270Connect` in API mode, use the following command:

#### Linux
```bash
docker run --rm -p 8080:8080 3270io/3270connect-linux:latest -api -api-port 8080
```

#### Windows
```bash
docker run --rm -p 8080:8080 3270io/3270connect-windows:latest -api -api-port 8080
```

### Web Dashboard Port

To specify the port for the web dashboard, use the `-dashboard-port` flag. The web dashboard server will only start if this flag is provided.

```bash
3270Connect -api -api-port 8080 -dashboard-port 8081
```

### Script Interface Port

To specify the port for the script interface, use the `-scriptport` flag. The script interface will only start if this flag is provided.

```bash
3270Connect -api -api-port 8080 -scriptport 5001
```

### 3270Connect API Usage

![type:video](3270Connect_API_1_0_4_0.mp4){: style=''}
