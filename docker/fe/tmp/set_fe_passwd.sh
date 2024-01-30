#!/bin/bash

QUERY_PORT=${FE_QUERY_PORT:-9030}
MYSELF=

if [ "x$POD_FQDN" == "x" ] ; then
    MYSELF=`hostname -f`
fi

log_stderr()
{
  echo "[`date`] $@" >& 2
}

set_passwd()
{
    local addr=$1
    local res=0
    timeout 15 mysql --connect-timeout 2 -h $addr -P $QUERY_PORT -uroot --skip-column-names --batch -e "SET PASSWORD FOR 'admin'@'%' = PASSWORD('$PASSWD');"
    timeout 15 mysql --connect-timeout 2 -h $addr -P $QUERY_PORT -uroot --skip-column-names --batch -e "SET PASSWORD FOR 'root'@'%' = PASSWORD('$PASSWD');"
    res=$?
    return $res
}

set_fe_passwd()
{
    local expire_timeout=120
    local start_time=`date +%s`
    local svc=$1

    while true
    do
        memlist=`timeout 15 mysql --connect-timeout 2 -h $svc -P $QUERY_PORT -uroot --skip-column-names --batch -e 'show frontends;'`
        local master=`echo "$memlist" | grep '\<FOLLOWER\>' | awk -F '\t' '{if ($8=="true") print $2}'`

        if [ "x$master" != "x" ] && [ "$master" == "$MYSELF" ]; then
            log_stderr "Find master to set passwd: $master!"
            set_passwd $svc
            return 0
        fi

        let "expire=start_time+expire_timeout"
        local now=`date +%s`
        if [[ $expire -le $now ]]; then
            log_stderr "Wait master is ready timeout!"
            return -1
        fi

        sleep 2
    done
}

set_fe_passwd $1

