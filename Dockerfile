FROM ubuntu:latest
LABEL authors="aomsir"

ENTRYPOINT ["top", "-b"]