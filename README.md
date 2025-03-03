# Setting Up a Dev Environment with kind

This document outlines the steps to launch a local kind cluster using Helm, with placeholders for secrets and file paths. Replace the placeholders with your actual values as needed.

## Prerequisites

- [brew](https://brew.sh/)
- Go (recommended version 1.18+)
- Helm

## Installation Steps

1. **Install kind**  
   ```
   brew install kind
   ```

2. **Install cloud-provider-kind**  
   ```
   go install sigs.k8s.io/cloud-provider-kind@latest
   ```

3. **Create the cluster configuration file**  
   Create a file, for example at `~/kind-cluster.yaml`, with the following content:

   ```
   kind: Cluster
   apiVersion: kind.x-k8s.io/v1alpha4
   nodes:
    - role: control-plane
      extraMounts:
        - containerPort: 80
          hostPort: 80
          protocol: TCP
        - containerPort: 443
          hostPort: 443
          protocol: TCP
   ```

4. **Create the kind cluster**  
   Run the following command (update the config path if necessary):

   ```
   kind create cluster --config /kind-cluster.yaml
   ```

5. **Run cloud-provider-kind**  
   To enable proper port mapping, execute:

   ```
   sudo ~/go/bin/cloud-provider-kind -enable-lb-port-mapping
   ```

6. **Configure Helm and Local Values**  
   In the Helm chart directory (for example, `helm/makaroni`), modify the `local-values.yaml` file. An example content:

   ```
   replicaCount: 1
   image:
   repository: "REPLACE_WITH_YOUR_REPO"   # Replace with your Docker repository
   tag: "latest"
   secret:
   name: "docker-secret"
   enabled: true
   dockerconfigjson: |
   {
     "auths": {
       "your-docker-registry.com": {
         "auth": "MY_DOCKER_SECRET"   # Replace MY_DOCKER_SECRET with your base64 encoded Docker secret
       }
     }
   }
   ```

7. **Deploy the Application with Helm**  
   Navigate to the Helm chart directory and execute:

   ```
   helm upgrade -i pasta . -f local-values.yaml
   ```

## Summary

After completing these steps, you will have a local dev environment up and running with kind.  
Remember to update all placeholders with your actual paths and secret values.
