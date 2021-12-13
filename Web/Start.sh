#!/bin/bash
echo you env is $1

if [ $1 == "TEST" ]
then

    docker stop verifyContract_testnet

    docker container rm verifyContract_testnet

    docker rmi verify_testnet:v1

    docker build -t verify_testnet:v1 .

    docker run --env RUNTIME="testnet" -itd --name verifyContract_testnet -p 1927:1927 verify_testnet:v1
fi

if [ $1 == "STAGING" ]
then

    docker stop verifyContract_mainnet

    docker container rm verifyContract_mainnet

    docker rmi verify_mainnet:v1

    docker build -t verify_mainnet:v1 .

    docker run --env RUNTIME="mainnet" -itd --name verifyContract_mainnet -p 1927:1927 verify_mainnet:v1
fi
