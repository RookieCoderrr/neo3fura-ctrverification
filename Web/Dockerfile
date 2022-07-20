FROM golang:1.15.6

ENV GO111MODULE="on"

ENV GOPROXY="https://goproxy.cn"

#ENV RUNTIME="mainnet"


RUN mkdir application

COPY . ./application

WORKDIR "application"

RUN  wget https://packages.microsoft.com/config/ubuntu/21.04/packages-microsoft-prod.deb -O packages-microsoft-prod.deb

RUN  dpkg -i packages-microsoft-prod.deb

RUN  rm packages-microsoft-prod.deb

RUN   apt-get update

RUN   apt-get install -y apt-transport-https

RUN   apt-get update

RUN  apt-get install -y dotnet-sdk-6.0

RUN  apt-get install -y aspnetcore-runtime-6.0

RUN  apt-get install -y  python3

RUN apt-get install -y python3-venv

##neo3-boa 0.11.3
RUN python3 -m venv boa113
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv113/bin/activate && pip install neo3-boa==0.11.3'
RUN /bin/sh -c 'source venv113/bin/deactivate'

##neo3-boa 0.11.2
RUN python3 -m venv venv112
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv112/bin/activate && pip install neo3-boa==0.11.2'
RUN /bin/sh -c 'source venv112/bin/deactivate'

##neo3-boa 0.11.1
RUN python3 -m venv venv111
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv111/bin/activate && pip install neo3-boa==0.11.1'
RUN /bin/sh -c 'source venv111/bin/deactivate'

##neo3-boa 0.11.0
RUN python3 -m venv venv110
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv110/bin/activate && pip install neo3-boa==0.11.0'
RUN /bin/sh -c 'source venv110/bin/deactivate'

##neo3-boa 0.10.1
RUN python3 -m venv venv101
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv101/bin/activate && pip install neo3-boa==0.10.1'
RUN /bin/sh -c 'source venv101/bin/deactivate'

##neo3-boa 0.10.0
RUN python3 -m venv venv100
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv100/bin/activate && pip install neo3-boa==0.10.0'
RUN /bin/sh -c 'source venv100/bin/deactivate'

##neo3-boa 0.9.0
RUN python3 -m venv venv090
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv090/bin/activate && pip install neo3-boa==0.9.0'
RUN /bin/sh -c 'source venv090/bin/deactivate'

##neo3-boa 0.8.3
RUN python3 -m venv venv083
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv083/bin/activate && pip install neo3-boa==0.8.3'
RUN /bin/sh -c 'source venv083/bin/deactivate'

##neo3-boa 0.8.2
RUN python3 -m venv venv082
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv082/bin/activate && pip install neo3-boa==0.8.2'
RUN /bin/sh -c 'source venv082/bin/deactivate'

##neo3-boa 0.8.1
RUN python3 -m venv venv081
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv081/bin/activate && pip install neo3-boa==0.8.1'
RUN /bin/sh -c 'source venv081/bin/deactivate'

##neo3-boa 0.8.0
RUN python3 -m venv venv080
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv080/bin/activate && pip install neo3-boa==0.8.0'
RUN /bin/sh -c 'source venv080/bin/deactivate'

##neo3-boa 0.7.1
RUN python3 -m venv venv071
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv071/bin/activate && pip install neo3-boa==0.7.1'
RUN /bin/sh -c 'source venv071/bin/deactivate'

##neo3-boa 0.7.0
RUN python3 -m venv venv070
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv070/bin/activate && pip install neo3-boa==0.7.0'
RUN /bin/sh -c 'source venv070/bin/deactivate'

##neo3-boa 0.3.0
RUN python3 -m venv venv030
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv030/bin/activate && pip install neo3-boa==0.3.0'
RUN /bin/sh -c 'source venv030/bin/deactivate'

##neo3-boa 0.0.3
RUN python3 -m venv venv003
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN deactivate
RUN /bin/sh -c 'source venv003/bin/activate && pip install neo3-boa==0.0.3'
RUN /bin/sh -c 'source venv003/bin/deactivate'

##neo3-boa 0.0.0
RUN python3 -m venv venv000
RUN echo "dash dash/sh boolean false" | debconf-set-selections
RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
RUN /bin/sh -c 'source venv000/bin/activate && pip install neo3-boa==0.0.0'
RUN /bin/sh -c 'source venv000/bin/deactivate'

RUN apt install -y openjdk-11-jre-headless

RUN apt install -y default-jdk

RUN tar -zxvf compiler2.tar.gz

RUN  chmod 777 compiler2/d/net6.0/nccs.exe

RUN  chmod 777 compiler2/e/net6.0/nccs.exe

RUN tar -zxvf contract.tar.gz

RUN wget https://github.com/nspcc-dev/neo-go/releases/download/v0.98.0/neo-go-linux-amd64

RUN chmod +x neo-go-linux-amd64

RUN mv neo-go-linux-amd64 /usr/bin/neo-go

#RUN export GOROOT="/usr/local/go"

RUN  go build -o main main.go

EXPOSE 1927

CMD ["./main"]
