# WarmPoolSpecTemplateSpec


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**active_deadline_seconds** | **int** |  | [optional] 
**affinity** | [**WarmPoolSpecTemplateSpecAffinity**](WarmPoolSpecTemplateSpecAffinity.md) |  | [optional] 
**automount_service_account_token** | **bool** |  | [optional] 
**containers** | [**List[WarmPoolSpecTemplateSpecContainersInner]**](WarmPoolSpecTemplateSpecContainersInner.md) |  | 
**dns_config** | [**WarmPoolSpecTemplateSpecDnsConfig**](WarmPoolSpecTemplateSpecDnsConfig.md) |  | [optional] 
**dns_policy** | **str** |  | [optional] 
**enable_service_links** | **bool** |  | [optional] 
**ephemeral_containers** | [**List[WarmPoolSpecTemplateSpecEphemeralContainersInner]**](WarmPoolSpecTemplateSpecEphemeralContainersInner.md) |  | [optional] 
**host_aliases** | [**List[WarmPoolSpecTemplateSpecHostAliasesInner]**](WarmPoolSpecTemplateSpecHostAliasesInner.md) |  | [optional] 
**host_ipc** | **bool** |  | [optional] 
**host_network** | **bool** |  | [optional] 
**host_pid** | **bool** |  | [optional] 
**host_users** | **bool** |  | [optional] 
**hostname** | **str** |  | [optional] 
**hostname_override** | **str** |  | [optional] 
**image_pull_secrets** | [**List[WarmPoolSpecTemplateSpecImagePullSecretsInner]**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | [optional] 
**init_containers** | [**List[WarmPoolSpecTemplateSpecContainersInner]**](WarmPoolSpecTemplateSpecContainersInner.md) |  | [optional] 
**node_name** | **str** |  | [optional] 
**node_selector** | **Dict[str, str]** |  | [optional] 
**os** | [**WarmPoolSpecTemplateSpecOs**](WarmPoolSpecTemplateSpecOs.md) |  | [optional] 
**overhead** | [**Dict[str, SandboxSpecResourcesLimitsValue]**](SandboxSpecResourcesLimitsValue.md) |  | [optional] 
**preemption_policy** | **str** |  | [optional] 
**priority** | **int** |  | [optional] 
**priority_class_name** | **str** |  | [optional] 
**readiness_gates** | [**List[WarmPoolSpecTemplateSpecReadinessGatesInner]**](WarmPoolSpecTemplateSpecReadinessGatesInner.md) |  | [optional] 
**resource_claims** | [**List[WarmPoolSpecTemplateSpecResourceClaimsInner]**](WarmPoolSpecTemplateSpecResourceClaimsInner.md) |  | [optional] 
**resources** | [**SandboxSpecResources**](SandboxSpecResources.md) |  | [optional] 
**restart_policy** | **str** |  | [optional] 
**runtime_class_name** | **str** |  | [optional] 
**scheduler_name** | **str** |  | [optional] 
**scheduling_gates** | [**List[WarmPoolSpecTemplateSpecOs]**](WarmPoolSpecTemplateSpecOs.md) |  | [optional] 
**security_context** | [**WarmPoolSpecTemplateSpecSecurityContext**](WarmPoolSpecTemplateSpecSecurityContext.md) |  | [optional] 
**service_account** | **str** |  | [optional] 
**service_account_name** | **str** |  | [optional] 
**set_hostname_as_fqdn** | **bool** |  | [optional] 
**share_process_namespace** | **bool** |  | [optional] 
**subdomain** | **str** |  | [optional] 
**termination_grace_period_seconds** | **int** |  | [optional] 
**tolerations** | [**List[WarmPoolSpecTemplateSpecTolerationsInner]**](WarmPoolSpecTemplateSpecTolerationsInner.md) |  | [optional] 
**topology_spread_constraints** | [**List[WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner]**](WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner.md) |  | [optional] 
**volumes** | [**List[WarmPoolSpecTemplateSpecVolumesInner]**](WarmPoolSpecTemplateSpecVolumesInner.md) |  | [optional] 
**workload_ref** | [**WarmPoolSpecTemplateSpecWorkloadRef**](WarmPoolSpecTemplateSpecWorkloadRef.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec import WarmPoolSpecTemplateSpec

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpec from a JSON string
warm_pool_spec_template_spec_instance = WarmPoolSpecTemplateSpec.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpec.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_dict = warm_pool_spec_template_spec_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpec from a dict
warm_pool_spec_template_spec_from_dict = WarmPoolSpecTemplateSpec.from_dict(warm_pool_spec_template_spec_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


