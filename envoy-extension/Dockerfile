FROM envoyproxy/envoy:v1.31-latest

# Copy the Envoy configuration file
COPY envoy_config.yaml /etc/envoy/envoy.yaml

CMD ["/usr/local/bin/envoy", "--config-path ", "/etc/envoy/envoy.yaml"]