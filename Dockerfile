FROM alpine
ARG TARGETPLATFORM
VOLUME "/data"
ENV PAPRIKA_DATA_DIR /data
ENTRYPOINT ["/usr/bin/paprika"]
COPY $TARGETPLATFORM/paprika /usr/bin/paprika
