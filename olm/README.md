### build bundles
```shell
docker build -t 172.22.96.158/system_containers/doris-bundles:v5.7 .
docker push 172.22.96.158/system_containers/doris-bundles:v5.7
```

### build catalogsource image

#### 构建新的registry
```
opm --skip-tls index add --bundles dev-registry.tenxcloud.com/system_containers/doris-bundles:v5.7 --tag 172.22.96.158/system_containers/doris-registry:v5.7 -c="docker"
docker push 172.22.96.158/system_containers/doris-registry:v5.7
```

### Images
```
172.22.96.158/system_containers/doris-registry:v5.7 镜像ID：a69ed3b59231
172.22.96.158/system_containers/doris-bundles:v5.7 镜像ID：ffdbf24d5cfc

172.22.96.158/system_containers/doris-operator:1.3.0 镜像ID：a7930d1a7de5
172.22.96.158/system_containers/doris-fe:2.0.2 镜像ID：8e19810cd37d
172.22.96.158/system_containers/doris-be:2.0.2 镜像ID：6b1f2109f5a7
```

