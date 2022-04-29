#!/bin/bash
echo you env is $1

if [ $1 == "TEST" ]
then

    docker stop verifyContract_testnet

    docker container rm verifyContract_testnet

    docker rmi verify_testnet:v1

    docker build -t verify_testnet:v1 .

    docker run --env RUNTIME="testnet" -itd --name verifyContract_testnet -p 3026:1927 verify_testnet:v1
fi

if [ $1 == "STAGING" ]
then

    docker stop verifyContract_mainnet

    docker container rm verifyContract_mainnet

    docker rmi verify_mainnet:v1

    docker build -t verify_mainnet:v1 .

    docker run --env RUNTIME="mainnet" -itd --name verifyContract_mainnet -p 3027:1927 verify_mainnet:v1
fi

if [ $1 == "TESTMAGNET" ]
then

    docker stop verifyContract_testmagnet

    docker container rm verifyContract_testmagnet

    docker rmi verify_testmagnet:v1

    docker build -t verify_testmagnet:v1 .

    docker run --env RUNTIME="testmagnet" -itd --name verifyContract_testmagnet -p 3028:1927 verify_testmagnet:v1
fi
