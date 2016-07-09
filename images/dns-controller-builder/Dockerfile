FROM debian:jessie

# Install packages:
#  curl (to download glide & golang)
#  git, mercurial (for go get)
RUN apt-get update && apt-get install --yes curl gcc make

# Install golang
RUN curl -L https://storage.googleapis.com/golang/go1.6.2.linux-amd64.tar.gz | tar zx -C /usr/local
ENV PATH $PATH:/usr/local/go/bin

COPY images/dns-controller-builder/onbuild.sh /onbuild.sh
