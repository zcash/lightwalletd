#!/bin/bash

for i in "$@"
do
case $i in
  -h|--help)
  echo HELP
  exit 0
  ;;
  -n=*|--network=*)
  NETWORK="${i#*=}"
  shift
  ;;
  *)
  echo Unknown option. Use -h for help.
  exit -1
  ;;
esac
done

if [ "$NETWORK" == "" ]
then
  echo ZCASHD_NETWORK=testnet
else
  echo ZCASHD_NETWORK=$NETWORK
fi

# sanity check openssl first...

if [ `openssl rand -base64 32 | wc -c` != 45 ] 
then 
  echo Openssl password generation failed.
  exit 1 
fi

PASSWORD_GRAFANA=`openssl rand -base64 32`
PASSWORD_ZCASHD=`openssl rand -base64 32`

while read TEMPLATE
do
  eval echo $TEMPLATE
done < .env.template
