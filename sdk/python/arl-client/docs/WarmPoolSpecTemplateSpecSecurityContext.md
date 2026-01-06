# WarmPoolSpecTemplateSpecSecurityContext


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**app_armor_profile** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextAppArmorProfile**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextAppArmorProfile.md) |  | [optional] 
**fs_group** | **int** |  | [optional] 
**fs_group_change_policy** | **str** |  | [optional] 
**run_as_group** | **int** |  | [optional] 
**run_as_non_root** | **bool** |  | [optional] 
**run_as_user** | **int** |  | [optional] 
**se_linux_change_policy** | **str** |  | [optional] 
**se_linux_options** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextSeLinuxOptions**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextSeLinuxOptions.md) |  | [optional] 
**seccomp_profile** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextAppArmorProfile**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextAppArmorProfile.md) |  | [optional] 
**supplemental_groups** | **List[int]** |  | [optional] 
**supplemental_groups_policy** | **str** |  | [optional] 
**sysctls** | [**List[WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGetHttpHeadersInner]**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGetHttpHeadersInner.md) |  | [optional] 
**windows_options** | [**WarmPoolSpecTemplateSpecContainersInnerSecurityContextWindowsOptions**](WarmPoolSpecTemplateSpecContainersInnerSecurityContextWindowsOptions.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_security_context import WarmPoolSpecTemplateSpecSecurityContext

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecSecurityContext from a JSON string
warm_pool_spec_template_spec_security_context_instance = WarmPoolSpecTemplateSpecSecurityContext.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecSecurityContext.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_security_context_dict = warm_pool_spec_template_spec_security_context_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecSecurityContext from a dict
warm_pool_spec_template_spec_security_context_from_dict = WarmPoolSpecTemplateSpecSecurityContext.from_dict(warm_pool_spec_template_spec_security_context_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


