# WarmPoolSpecTemplateSpecContainersInnerLivenessProbe


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_exec** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartExec**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartExec.md) |  | [optional] 
**failure_threshold** | **int** |  | [optional] 
**grpc** | [**WarmPoolSpecTemplateSpecContainersInnerLivenessProbeGrpc**](WarmPoolSpecTemplateSpecContainersInnerLivenessProbeGrpc.md) |  | [optional] 
**http_get** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet.md) |  | [optional] 
**initial_delay_seconds** | **int** |  | [optional] 
**period_seconds** | **int** |  | [optional] 
**success_threshold** | **int** |  | [optional] 
**tcp_socket** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartTcpSocket**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartTcpSocket.md) |  | [optional] 
**termination_grace_period_seconds** | **int** |  | [optional] 
**timeout_seconds** | **int** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_liveness_probe import WarmPoolSpecTemplateSpecContainersInnerLivenessProbe

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerLivenessProbe from a JSON string
warm_pool_spec_template_spec_containers_inner_liveness_probe_instance = WarmPoolSpecTemplateSpecContainersInnerLivenessProbe.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerLivenessProbe.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_liveness_probe_dict = warm_pool_spec_template_spec_containers_inner_liveness_probe_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerLivenessProbe from a dict
warm_pool_spec_template_spec_containers_inner_liveness_probe_from_dict = WarmPoolSpecTemplateSpecContainersInnerLivenessProbe.from_dict(warm_pool_spec_template_spec_containers_inner_liveness_probe_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


