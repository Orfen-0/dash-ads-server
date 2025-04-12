  logging {
    level  = "info"
    format = "logfmt"
  }

    loki.source.file "go_app_logs" {
    targets = [
    {
    __path__ = "/var/log/myapp/server.log",
    job      = "go-server"
    },
    ]
    forward_to = [loki.write.default.receiver]
  }

    loki.write "default" {
    endpoint {
    url = "http://loki:3100/loki/api/v1/push"
    }
  }
