#!/bin/bash
echo you env is $1

if [ $1 == "TEST" ]
then

    docker stop verifyContract

    docker container rm verifyContract

    docker rmi verify:v1

    docker build -t verify:v1 .

    docker run --env RUNTIME="testnet" -itd --name verifyContract -p 1927:1927 verify:v1
fi

if [ $1 == "MAIN" ]
then

    docker stop verifyContract

    docker container rm verifyContract

    docker rmi verify:v1

    docker build -t verify:v1 .

    docker run --env RUNTIME="mainnet" -itd --name verifyContract -p 1927:1927 verify:v1
fi
