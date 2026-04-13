# GCE API Client & Infrastructure Interface

## Overview

The GCE API Client module encapsulates interactions with Google Cloud. This implementation makes direct, authenticated HTTP REST API calls to the Compute Engine endpoints.

This document details the exact REST endpoints, JSON payloads, and authentication mechanics required to provision TPU VMs, Workload Policies, and Managed Instance Groups (MIGs).

---

## Authentication & Base Client

**Implementation Note:** The implementation temporarily uses direct, authenticated HTTP REST API calls via `net/http` rather than the generated `google.golang.org/api/compute/v1` SDK. This is because TPU features (like `TargetSizePolicy` in MIGs and `AcceleratorTopology` in Workload Policies) are not yet fully released in the standard generated Go SDK structs.

All requests to GCE are made over standard `net/http` using OAuth2 credentials.

**Library:** `google3/third_party/golang/oauth2/google/google`
**Scope:** `https://www.googleapis.com/auth/cloud-platform`
**Base URL:** `https://compute.googleapis.com/compute`

The client supports fetching credentials via Application Default Credentials (ADC) or by pointing to a specific service account JSON key via the `GCE_KEY_PATH` environment variable.

### Raw Request Wrapper
The implementation utilizes a generic `doRequest(method, url, body)` function that marshals Go structs/maps into JSON, attaches the OAuth2 bearer token, sets `Content-Type: application/json`, and parses the HTTP status code and response body.

---

## 1. Instance Templates

If the user does not provide an existing template, the controller constructs and POSTs a new one to the global API.

### Check Existence
*   **Method/URL:** `GET {BaseURL}/v1/projects/{project}/global/instanceTemplates/{templateName}`
*   **Action:** If `200 OK`, the template exists; proceed. If `404 Not Found`, proceed to creation.

### Creation Payload
*   **Method/URL:** `POST {BaseURL}/v1/projects/{project}/global/instanceTemplates`
*   **JSON Payload Structure:**
    ```json
    {
      "name": "tpunodegroup-{CRD_NAME}-it",
      "properties": {
        "machineType": "{CRD_MACHINE_TYPE}",
        "canIpForward": false,
        "tags": { "items": ["tpu-node"] },
        "disks": [{
          "boot": true,
          "autoDelete": true,
          "mode": "READ_WRITE",
          "type": "PERSISTENT",
          "initializeParams": {
            "diskSizeGb": "{CRD_DISK_SIZE}",
            "sourceImage": "{CRD_IMAGE_URI_OR_DEFAULT}"
          }
        }],
        "networkInterfaces": [{
          "subnetwork": "{CRD_SUBNETWORK_URI}",
          "accessConfigs": [{"type": "ONE_TO_ONE_NAT", "name": "external-nat"}]
        }],
        "serviceAccounts": [{
          "email": "default",
          "scopes": ["https://www.googleapis.com/auth/cloud-platform"]
        }],
        "scheduling": {
          "onHostMaintenance": "TERMINATE",
          "automaticRestart": true
        }
      }
    }
    ```
    *(Note: If `spec.image` is omitted in the CRD, `{CRD_IMAGE_URI_OR_DEFAULT}` defaults to `projects/ml-images/global/images/family/tpu-ubuntu2204-base`)*

#### Reservation Binding
If `provisioningModel` is `reservation-bound`, mutate the `scheduling` block before sending the POST request:
```json
"scheduling": {
  "provisioningModel": "RESERVATION_BOUND",
  "instanceTerminationAction": "DELETE"
},
"reservationAffinity": {
  "consumeReservationType": "SPECIFIC_RESERVATION",
  "key": "compute.googleapis.com/reservation-name",
  "values": ["{CRD_RESERVATION_NAME}"]
}
```

---

## 2. Workload Policies

Required for multi-host static slices to define the physical accelerator topology.

*   *Note on Regions:* The region is extracted by stripping the trailing zone identifier from `spec.location` (e.g., `us-central1-a` becomes `us-central1`).

### Check Existence
*   **Method/URL:** `GET {BaseURL}/v1/projects/{project}/regions/{region}/resourcePolicies/{policyName}`

### Creation Payload
*   **Method/URL:** `POST {BaseURL}/v1/projects/{project}/regions/{region}/resourcePolicies`
*   **JSON Payload Structure:**
    ```json
    {
      "name": "tpunodegroup-{CRD_NAME}-wp",
      "region": "{REGION}",
      "workloadPolicy": {
        "type": "HIGH_THROUGHPUT",
        "acceleratorTopology": "{CRD_TOPOLOGY}"
      }
    }
    ```

---

## 3. Managed Instance Groups (MIGs)

**CRITICAL:** The MIG creation relies on the Compute Engine **`/beta`** endpoint to access advanced target sizing and bulk creation policies for TPUs.

### Check Existence
*   **Method/URL:** `GET {BaseURL}/beta/projects/{project}/zones/{zone}/instanceGroupManagers/{migName}`

### Creation Payload
*   **Method/URL:** `POST {BaseURL}/beta/projects/{project}/zones/{zone}/instanceGroupManagers`
*   **JSON Payload Structure:**
    *(Note: Because `TPUNodeGroup` is a Cluster-scoped CRD, its `{CRD_NAME}` is guaranteed unique across the cluster, preventing naming collisions when creating GCE resources).*
    ```json
    {
      "name": "tpunodegroup-{CRD_NAME}-mig",
      "baseInstanceName": "tpunodegroup-{CRD_NAME}-mig",
      "instanceTemplate": "https://compute.googleapis.com/compute/beta/projects/{project}/global/instanceTemplates/{TEMPLATE_NAME}",
      "targetSize": {CRD_NODE_COUNT},
      "zone": "{CRD_ZONE}",
      "allInstancesConfig": {
        "properties": {
          "metadata": {
            "items": [
              {
                "key": "cloud.google.com/gk8s-tpu-topology",
                "value": "{CRD_TOPOLOGY}"
              },
              {
                "key": "cloud.google.com/gk8s-tpu-accelerator",
                "value": "{CRD_MACHINE_TYPE}"
              },
              {
                "key": "cloud.google.com/gk8s-tpu-slice-{CRD_TOPOLOGY}-id",
                "value": "{UNIQUE_SLICE_UUID}"
              },
              {
                "key": "startup-script",
                "value": "{INJECTED_STARTUP_SCRIPT}"
              }
            ]
          }
        }
      },
      "instanceLifecyclePolicy": {
        "defaultActionOnFailure": "DO_NOTHING"
      },
      "targetSizePolicy": {
        "mode": "BULK"
      }
    }
    ```
    *(Note 2: The `startup-script` item in `allInstancesConfig` is only injected if `spec.bootstrapKubernetes: true`)*

#### Workload Policy Attachment
If a Workload Policy was created (multi-host slice), append it to the MIG payload:
```json
"resourcePolicies": {
  "workloadPolicy": "projects/{project}/regions/{region}/resourcePolicies/{POLICY_NAME}"
}
```

---

## Error Handling

*   **Idempotency:** A `200 OK` on a `GET` request indicates the resource exists, and the creation `POST` is skipped.
*   **API Failures:** Any non-`200 OK` (or `20x`) response on a `POST` is surfaced to the reconciler. The raw HTTP status and the stringified `respBody` are included in the error message to facilitate debugging.