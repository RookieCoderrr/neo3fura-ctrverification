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

#RUN apt-get install -y python-pip

RUN python3 -m venv venv
#RUN echo "dash dash/sh boolean false" | debconf-set-selections
#RUN DEBIAN_FRONTEND=noninteractive dpkg-reconfigure dash
#
#RUN source venv/bin/activate

#RUN pip install neo3-boa

#RUN git clone https://github.com/CityOfZion/neo3-boa.git
#
#RUN pip install wheel
#
#RUN pip install -e .

#RUN  chmod 777 a/nccs
#
#RUN  chmod 777 c/nccs
#
#RUN  chmod 777 b/nccs

RUN  chmod 777 d/nccs

RUN  go build -o main main.go

EXPOSE 1927

CMD ["./main"]
