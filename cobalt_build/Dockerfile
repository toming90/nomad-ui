#FROM scratch
FROM registry.mo.sap.corp:5002/cobalt/alpine34-node445

ADD ./nomad-ui-linux-amd64 /nomad-ui
COPY  config/containerpilot.json   /opt/containerpilot/app.json

EXPOSE 3000

CMD ["/nomad-ui"]
