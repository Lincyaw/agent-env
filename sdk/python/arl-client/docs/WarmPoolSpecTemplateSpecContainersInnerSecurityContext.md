# WarmPoolSpecTemplateSpecContainersInnerSecurityContext


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**allow_privilege_escalation** | **bool** |  | [optional] 
**app_armor_profile** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextAppArmorProfile**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextAppArmorProfile.md) |  | [optional] 
**capabilities** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextCapabilities**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextCapabilities.md) |  | [optional] 
**privileged** | **bool** |  | [optional] 
**proc_mount** | **str** |  | [optional] 
**read_only_root_filesystem** | **bool** |  | [optional] 
**run_as_group** | **int** |  | [optional] 
**run_as_non_root** | **bool** |  | [optional] 
**run_as_user** | **int** |  | [optional] 
**se_linux_options** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextSeLinuxOptions**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextSeLinuxOptions.md) |  | [optional] 
**seccomp_profile** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextAppArmorProfile**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextAppArmorProfile.md) |  | [optional] 
**windows_options** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextWindowsOptions**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextWindowsOptions.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_security_context import WarmPoolSpecTemplateSpecContainersInnerSecurityContext

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerSecurityContext from a JSON string
warm_pool_spec_template_spec_containers_inner_security_context_instance = WarmPoolSpecTemplateSpecContainersInnerSecurityContext.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerSecurityContext.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_security_context_dict = warm_pool_spec_template_spec_containers_inner_security_context_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerSecurityContext from a dict
warm_pool_spec_template_spec_containers_inner_security_context_from_dict = WarmPoolSpecTemplateSpecContainersInnerSecurityContext.from_dict(warm_pool_spec_template_spec_containers_inner_security_context_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


