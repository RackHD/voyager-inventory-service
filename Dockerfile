FROM ubuntu:16.04

ADD bin/voyager-inventory-service voyager-inventory-service

CMD ./voyager-inventory-service
