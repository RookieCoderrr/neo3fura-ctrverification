#!/bin/bash
cd javacontractgradle/build/neow3j
rm -f *
cd ../..

cd javacontractgradle
rm -f build.gradle
cd ..
cp $2/build.gradle javacontractgradle/

#sed -i "28c className ="$1"" build.gradle

OPTS=""
SLASH="/"
JAVA=".java"
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

cd javacontractgradle/src/main/java

rm -rf *

mkdir -p $Package

cd $Package

cd /go/application

cp $2/*.java javacontractgradle/src/main/java/$Package/

cd /go/application/javacontractgradle

./gradlew neow3jCompile
