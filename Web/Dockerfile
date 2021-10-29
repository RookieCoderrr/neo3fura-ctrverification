FROM golang:1.15.6

ENV GO111MODULE="on"

ENV GOPROXY="https://goproxy.cn"


ENV RUNTIME="mainnet"

RUN mkdir application

COPY . ./application

WORKDIR "application"


#RUN chmod 777 dotnet-install.sh
#
#RUN ./dotnet-install.sh -c 5.0

#RUN snap version
#RUN snap install dotnet-runtime-50 --classic
#
#RUN snap alias dotnet-runtime-50.dotnet dotnet

#RUN export DOTNET_ROOT=/snap/dotnet-sdk/current

#RUN  apt-get update; \
#       apt-get install -y apt-transport-https && \
#       apt-get update && \
#       apt-get install -y dotnet-runtime-5.0


#RUN dotnet tool install --tool-path a Neo.Compiler.CSharp --version 3.0.0
#
#RUN dotnet tool install --tool-path b Neo.Compiler.CSharp --version 3.0.2
#
#RUN dotnet tool install --tool-path c Neo.Compiler.CSharp --version 3.0.3

RUN  go build -o main main.go

EXPOSE 1927

CMD ["./main"]
