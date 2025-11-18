FROM gcr.io/distroless/static
ARG TARGETPLATFORM
VOLUME "/data"
ENV PAPRIKA_DATA_DIR /data
COPY $TARGETPLATFORM/paprika /usr/bin/paprika
ENTRYPOINT ["/usr/bin/paprika"]
