FROM decred/decred-golang-builder-1.9 as builder
COPY . /go/src/github.com/decred/dcrwallet
WORKDIR /go/src/github.com/decred/dcrwallet
RUN dep ensure
ENV CGO_ENABLED=0
RUN mkdir -p /tmp/output
RUN go build -ldflags "-s -w" -a -tags netgo -o /tmp/output/dcrwallet .
WORKDIR /go/src/github.com/decred/dcrwallet/cmd
RUN for cmd in `echo *`; do go build -ldflags "-s -w" -a -tags netgo -o /tmp/output/$cmd ./$cmd; done

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /tmp/output/* /usr/local/bin/
EXPOSE 9110
EXPOSE 9111
VOLUME /etc/dcrwallet
VOLUME /var/log/dcrwallet
VOLUME /var/lib/dcrwallet
ENTRYPOINT [ "/usr/local/bin/dcrwallet", "--configfile=/etc/dcrwallet/config", "--rpccert=/etc/dcrwallet/rpc.cert", "--rpckey=/etc/dcrwallet/rpc.key", "--grpclisten=9110", "--rpclisten=9111", "--logdir=/var/log/dcrwallet", "--appdata=/var/lib/dcrwallet" ]