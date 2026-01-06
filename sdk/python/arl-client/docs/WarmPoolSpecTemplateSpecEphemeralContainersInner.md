# WarmPoolSpecTemplateSpecEphemeralContainersInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**args** | **List[str]** |  | [optional] 
**command** | **List[str]** |  | [optional] 
**env** | [**List[WarmPoolSpecTemplateSpecContainersInnerEnvInner]**](WarmPoolSpecTemplateSpecContainersInnerEnvInner.md) |  | [optional] 
**env_from** | [**List[WarmPoolSpecTemplateSpecContainersInnerEnvFromInner]**](WarmPoolSpecTemplateSpecContainersInnerEnvFromInner.md) |  | [optional] 
**image** | **str** |  | [optional] 
**image_pull_policy** | **str** |  | [optional] 
**lifecycle** | [**WarmPoolSpecTemplateSpecContainersInnerLifecycle**](WarmPoolSpecTemplateSpecContainersInnerLifecycle.md) |  | [optional] 
**liveness_probe** | [**WarmPoolSpecTemplateSpecContainersInnerLivenessProbe**](WarmPoolSpecTemplateSpecContainersInnerLivenessProbe.md) |  | [optional] 
**name** | **str** |  | 
**ports** | [**List[WarmPoolSpecTemplateSpecContainersInnerPortsInner]**](WarmPoolSpecTemplateSpecContainersInnerPortsInner.md) |  | [optional] 
**readiness_probe** | [**WarmPoolSpecTemplateSpecContainersInnerLivenessProbe**](WarmPoolSpecTemplateSpecContainersInnerLivenessProbe.md) |  | [optional] 
**resize_policy** | [**List[WarmPoolSpecTemplateSpecContainersInnerResizePolicyInner]**](WarmPoolSpecTemplateSpecContainersInnerResizePolicyInner.md) |  | [optional] 
**resources** | [**SandboxSpecResources**](SandboxSpecResources.md) |  | [optional] 
**restart_policy** | **str** |  | [optional] 
**restart_policy_rules** | [**List[WarmPoolSpecTemplateSpecContainersInnerRestartPolicyRulesInner]**](WarmPoolSpecTemplateSpecContainersInnerRestartPolicyRulesInner.md) |  | [optional] 
**security_context** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContext**](WarmPoolSpecTemplateSpecContainersInnerSecurityContext.md) |  | [optional] 
**startup_probe** | [**WarmPoolSpecTemplateSpecContainersInnerLivenessProbe**](WarmPoolSpecTemplateSpecContainersInnerLivenessProbe.md) |  | [optional] 
**stdin** | **bool** |  | [optional] 
**stdin_once** | **bool** |  | [optional] 
**target_container_name** | **str** |  | [optional] 
**termination_message_path** | **str** |  | [optional] 
**termination_message_policy** | **str** |  | [optional] 
**tty** | **bool** |  | [optional] 
**volume_devices** | [**List[WarmPoolSpecTemplateSpecContainersInnerVolumeDevicesInner]**](WarmPoolSpecTemplateSpecContainersInnerVolumeDevicesInner.md) |  | [optional] 
**volume_mounts** | [**List[WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner]**](WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner.md) |  | [optional] 
**working_dir** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_ephemeral_containers_inner import WarmPoolSpecTemplateSpecEphemeralContainersInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecEphemeralContainersInner from a JSON string
warm_pool_spec_template_spec_ephemeral_containers_inner_instance = WarmPoolSpecTemplateSpecEphemeralContainersInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecEphemeralContainersInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_ephemeral_containers_inner_dict = warm_pool_spec_template_spec_ephemeral_containers_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecEphemeralContainersInner from a dict
warm_pool_spec_template_spec_ephemeral_containers_inner_from_dict = WarmPoolSpecTemplateSpecEphemeralContainersInner.from_dict(warm_pool_spec_template_spec_ephemeral_containers_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


