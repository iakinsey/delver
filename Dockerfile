FROM golang:buster

RUN mkdir /build
COPY ./delver-repo.tar.gz /build
WORKDIR /build
RUN tar xvf delver-repo.tar.gz
RUN make
