#!/bin/bash

PASSWD_PATH="/opt/apache-doris/fe/doris-meta/passwd"
QUERY_PORT=${FE_QUERY_PORT:-9030}
OLD_PASSWD=
MYSELF=

if [ "x$POD_FQDN" == "x" ] ; then
    MYSELF=`hostname -f`
fi

if [ -f $PASSWD_PATH ]; then
    OLD_PASSWD=`cat $PASSWD_PATH`
fi

log_stderr()
{
  echo "[`date`] $@" >& 2
}

set_passwd()
{
    local res=0
    if [ "$PASSWD" != "$OLD_PASSWD" ]; then
        timeout 15 mysql --connect-timeout 2 -h127.0.0.1 -P$QUERY_PORT -uroot --skip-column-names --batch -e "SET PASSWORD FOR 'admin'@'%' = PASSWORD('$PASSWD');"
        timeout 15 mysql --connect-timeout 2 -h127.0.0.1 -P$QUERY_PORT -uroot --skip-column-names --batch -e "SET PASSWORD FOR 'root'@'%' = PASSWORD('$PASSWD');"
        res=$?

        if [ $res -eq 0 ]; then
            log_stderr "Update password is ok!"
            echo -n "$PASSWD" > $PASSWD_PATH
        fi
    fi
    return $res
}

set_fe_passwd()
{
    local expire_timeout=120
    local start_time=`date +%s`

    while true
    do
        memlist=`timeout 15 mysql --connect-timeout 2 -h127.0.0.1 -P$QUERY_PORT -uroot --skip-column-names --batch -e 'show frontends;'`
        local master=`echo "$memlist" | grep '\<FOLLOWER\>' | awk -F '\t' '{if ($8=="true") print $2}'`

        if [ "x$master" != "x" ]; then
            log_stderr "Find master node: $master"
            # if [ "$master" == "$MYSELF" ]; then
            #     set_passwd
            #     return 0
            # fi
            #log_stderr "Current node is follower: $MYSELF"
            set_passwd
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

set_fe_passwd

