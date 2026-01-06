# arl_client.DefaultApi

All URIs are relative to *http://localhost*

Method | HTTP request | Description
------------- | ------------- | -------------
[**create_sandbox**](DefaultApi.md#create_sandbox) | **POST** /api/v1alpha1/namespaces/{namespace}/sandboxes | Create Sandbox
[**create_task**](DefaultApi.md#create_task) | **POST** /api/v1alpha1/namespaces/{namespace}/tasks | Create Task
[**create_warm_pool**](DefaultApi.md#create_warm_pool) | **POST** /api/v1alpha1/namespaces/{namespace}/warmpools | Create WarmPool
[**list_sandboxes**](DefaultApi.md#list_sandboxes) | **GET** /api/v1alpha1/namespaces/{namespace}/sandboxes | List Sandboxes
[**list_tasks**](DefaultApi.md#list_tasks) | **GET** /api/v1alpha1/namespaces/{namespace}/tasks | List Tasks
[**list_warm_pools**](DefaultApi.md#list_warm_pools) | **GET** /api/v1alpha1/namespaces/{namespace}/warmpools | List WarmPools


# **create_sandbox**
> Sandbox create_sandbox(namespace, sandbox)

Create Sandbox

### Example


```python
import arl_client
from arl_client.models.sandbox import Sandbox
from arl_client.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = arl_client.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with arl_client.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = arl_client.DefaultApi(api_client)
    namespace = 'namespace_example' # str | 
    sandbox = arl_client.Sandbox() # Sandbox | 

    try:
        # Create Sandbox
        api_response = api_instance.create_sandbox(namespace, sandbox)
        print("The response of DefaultApi->create_sandbox:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling DefaultApi->create_sandbox: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **namespace** | **str**|  | 
 **sandbox** | [**Sandbox**](Sandbox.md)|  | 

### Return type

[**Sandbox**](Sandbox.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**201** | Sandbox created |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_task**
> Task create_task(namespace, task)

Create Task

### Example


```python
import arl_client
from arl_client.models.task import Task
from arl_client.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = arl_client.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with arl_client.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = arl_client.DefaultApi(api_client)
    namespace = 'namespace_example' # str | 
    task = arl_client.Task() # Task | 

    try:
        # Create Task
        api_response = api_instance.create_task(namespace, task)
        print("The response of DefaultApi->create_task:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling DefaultApi->create_task: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **namespace** | **str**|  | 
 **task** | [**Task**](Task.md)|  | 

### Return type

[**Task**](Task.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**201** | Task created |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **create_warm_pool**
> WarmPool create_warm_pool(namespace, warm_pool)

Create WarmPool

### Example


```python
import arl_client
from arl_client.models.warm_pool import WarmPool
from arl_client.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = arl_client.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with arl_client.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = arl_client.DefaultApi(api_client)
    namespace = 'namespace_example' # str | 
    warm_pool = arl_client.WarmPool() # WarmPool | 

    try:
        # Create WarmPool
        api_response = api_instance.create_warm_pool(namespace, warm_pool)
        print("The response of DefaultApi->create_warm_pool:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling DefaultApi->create_warm_pool: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **namespace** | **str**|  | 
 **warm_pool** | [**WarmPool**](WarmPool.md)|  | 

### Return type

[**WarmPool**](WarmPool.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: application/json
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**201** | WarmPool created |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_sandboxes**
> SandboxList list_sandboxes(namespace)

List Sandboxes

### Example


```python
import arl_client
from arl_client.models.sandbox_list import SandboxList
from arl_client.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = arl_client.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with arl_client.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = arl_client.DefaultApi(api_client)
    namespace = 'namespace_example' # str | 

    try:
        # List Sandboxes
        api_response = api_instance.list_sandboxes(namespace)
        print("The response of DefaultApi->list_sandboxes:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling DefaultApi->list_sandboxes: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **namespace** | **str**|  | 

### Return type

[**SandboxList**](SandboxList.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | List of sandboxes |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_tasks**
> TaskList list_tasks(namespace)

List Tasks

### Example


```python
import arl_client
from arl_client.models.task_list import TaskList
from arl_client.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = arl_client.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with arl_client.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = arl_client.DefaultApi(api_client)
    namespace = 'namespace_example' # str | 

    try:
        # List Tasks
        api_response = api_instance.list_tasks(namespace)
        print("The response of DefaultApi->list_tasks:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling DefaultApi->list_tasks: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **namespace** | **str**|  | 

### Return type

[**TaskList**](TaskList.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | List of tasks |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

# **list_warm_pools**
> WarmPoolList list_warm_pools(namespace)

List WarmPools

### Example


```python
import arl_client
from arl_client.models.warm_pool_list import WarmPoolList
from arl_client.rest import ApiException
from pprint import pprint

# Defining the host is optional and defaults to http://localhost
# See configuration.py for a list of all supported configuration parameters.
configuration = arl_client.Configuration(
    host = "http://localhost"
)


# Enter a context with an instance of the API client
with arl_client.ApiClient(configuration) as api_client:
    # Create an instance of the API class
    api_instance = arl_client.DefaultApi(api_client)
    namespace = 'namespace_example' # str | 

    try:
        # List WarmPools
        api_response = api_instance.list_warm_pools(namespace)
        print("The response of DefaultApi->list_warm_pools:\n")
        pprint(api_response)
    except Exception as e:
        print("Exception when calling DefaultApi->list_warm_pools: %s\n" % e)
```



### Parameters


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **namespace** | **str**|  | 

### Return type

[**WarmPoolList**](WarmPoolList.md)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: application/json

### HTTP response details

| Status code | Description | Response headers |
|-------------|-------------|------------------|
**200** | List of warmpools |  -  |

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

