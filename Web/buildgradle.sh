#!/bin/bash

cd /go/application/javacontractgradle

sed -i "28c className ="$1"" build.gradle

OPTS=""
SLASH="/"
var=$1
var=${var//./ }  
for element in $var
do
    OPTS="$OPTS$element$SLASH"
done
OPTS=${OPTS%?}
echo $OPTS
ClassName=${OPTS##*/}
echo $ClassName

Package=${OPTS%/*}
echo $Package

cd src/main/java

rm -rf *

mkdir -p $Package

cd $Package

cp /go/application/$2/$ClassName go/appliaction/javacontractgradle/src/main/java/$Pacakge

cd /go/application/javaContractDemo

./gradlew neow3jCompile
