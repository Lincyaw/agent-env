# Python Architecture Patterns

## Project Architecture Principles

### Separation of Concerns
Organize code by functionality, not by layer.

**Feature-based structure:**
```
src/package/
├── users/
│   ├── __init__.py
│   ├── models.py      # User domain models
│   ├── service.py     # Business logic
│   ├── repository.py  # Data access
│   └── api.py         # API endpoints
├── orders/
│   ├── __init__.py
│   ├── models.py
│   ├── service.py
│   ├── repository.py
│   └── api.py
└── shared/
    ├── __init__.py
    ├── database.py
    └── utils.py
```

### Dependency Injection

Use protocols and dependency injection for testability:

```python
from typing import Protocol
from collections.abc import Sequence

class UserRepository(Protocol):
    def get_by_id(self, user_id: int) -> User | None: ...
    def save(self, user: User) -> None: ...

class UserService:
    def __init__(self, repository: UserRepository) -> None:
        self.repository = repository
    
    def activate_user(self, user_id: int) -> None:
        user = self.repository.get_by_id(user_id)
        if user is None:
            raise UserNotFoundError(user_id)
        user.activate()
        self.repository.save(user)
```

### Configuration Management

Use Pydantic for type-safe configuration:

```python
from pydantic_settings import BaseSettings
from pydantic import Field, PostgresDsn

class Settings(BaseSettings):
    database_url: PostgresDsn
    api_key: str = Field(min_length=32)
    debug: bool = False
    max_connections: int = 10
    
    model_config = {
        "env_file": ".env",
        "env_file_encoding": "utf-8",
    }

settings = Settings()
```

## Design Patterns

### Factory Pattern

```python
from abc import ABC, abstractmethod
from typing import Protocol

class DataProcessor(Protocol):
    def process(self, data: bytes) -> dict: ...

class JSONProcessor:
    def process(self, data: bytes) -> dict:
        return json.loads(data)

class XMLProcessor:
    def process(self, data: bytes) -> dict:
        # Parse XML and convert to dict
        ...

class ProcessorFactory:
    _processors: dict[str, type[DataProcessor]] = {
        "json": JSONProcessor,
        "xml": XMLProcessor,
    }
    
    @classmethod
    def create(cls, format: str) -> DataProcessor:
        if format not in cls._processors:
            raise ValueError(f"Unknown format: {format}")
        return cls._processors[format]()
```

### Strategy Pattern

```python
from typing import Protocol

class PricingStrategy(Protocol):
    def calculate_price(self, base_price: float) -> float: ...

class RegularPricing:
    def calculate_price(self, base_price: float) -> float:
        return base_price

class DiscountPricing:
    def __init__(self, discount_percent: float) -> None:
        self.discount = discount_percent
    
    def calculate_price(self, base_price: float) -> float:
        return base_price * (1 - self.discount / 100)

class Order:
    def __init__(
        self,
        items: list[Item],
        pricing: PricingStrategy,
    ) -> None:
        self.items = items
        self.pricing = pricing
    
    def total(self) -> float:
        base = sum(item.price for item in self.items)
        return self.pricing.calculate_price(base)
```

### Repository Pattern

```python
from abc import ABC, abstractmethod
from typing import Optional
from collections.abc import Sequence

class Repository(ABC):
    @abstractmethod
    def get(self, id: int) -> Optional[T]: ...
    
    @abstractmethod
    def list(self, limit: int = 100) -> Sequence[T]: ...
    
    @abstractmethod
    def save(self, entity: T) -> None: ...
    
    @abstractmethod
    def delete(self, id: int) -> None: ...

class SQLUserRepository(Repository):
    def __init__(self, session: Session) -> None:
        self.session = session
    
    def get(self, id: int) -> Optional[User]:
        return self.session.query(User).filter_by(id=id).first()
    
    def list(self, limit: int = 100) -> Sequence[User]:
        return self.session.query(User).limit(limit).all()
    
    def save(self, user: User) -> None:
        self.session.add(user)
        self.session.commit()
    
    def delete(self, id: int) -> None:
        user = self.get(id)
        if user:
            self.session.delete(user)
            self.session.commit()
```

### Unit of Work Pattern

```python
from contextlib import contextmanager
from typing import Generator

class UnitOfWork:
    def __init__(self, session_factory: callable) -> None:
        self.session_factory = session_factory
        self.session = None
    
    def __enter__(self):
        self.session = self.session_factory()
        return self
    
    def __exit__(self, exc_type, exc_val, exc_tb):
        if exc_type is not None:
            self.session.rollback()
        else:
            self.session.commit()
        self.session.close()
    
    @property
    def users(self) -> UserRepository:
        return SQLUserRepository(self.session)

# Usage
with UnitOfWork(session_factory) as uow:
    user = uow.users.get(user_id)
    user.update_email(new_email)
    uow.users.save(user)
```

## API Design

### RESTful API with FastAPI

```python
from fastapi import FastAPI, Depends, HTTPException, status
from pydantic import BaseModel

app = FastAPI()

class UserCreate(BaseModel):
    username: str
    email: str

class UserResponse(BaseModel):
    id: int
    username: str
    email: str
    
    model_config = {"from_attributes": True}

def get_user_service() -> UserService:
    # Dependency injection
    return UserService(repository=get_repository())

@app.post("/users", response_model=UserResponse, status_code=status.HTTP_201_CREATED)
async def create_user(
    user_data: UserCreate,
    service: UserService = Depends(get_user_service),
) -> UserResponse:
    try:
        user = service.create_user(
            username=user_data.username,
            email=user_data.email,
        )
        return UserResponse.model_validate(user)
    except DuplicateUserError as e:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail=str(e),
        )
```

## Async Patterns

### Async Context Managers

```python
from contextlib import asynccontextmanager
import aiohttp

@asynccontextmanager
async def api_client(base_url: str):
    async with aiohttp.ClientSession() as session:
        client = APIClient(session, base_url)
        try:
            yield client
        finally:
            await client.cleanup()

# Usage
async with api_client("https://api.example.com") as client:
    result = await client.fetch_data()
```

### Task Management

```python
import asyncio
from typing import Any

async def process_batch(items: list[Any]) -> list[Any]:
    """Process items concurrently with controlled concurrency."""
    semaphore = asyncio.Semaphore(10)  # Max 10 concurrent tasks
    
    async def process_with_semaphore(item):
        async with semaphore:
            return await process_item(item)
    
    tasks = [process_with_semaphore(item) for item in items]
    return await asyncio.gather(*tasks, return_exceptions=True)
```

## Error Handling Architecture

### Custom Exception Hierarchy

```python
class ApplicationError(Exception):
    """Base exception for all application errors."""
    pass

class ValidationError(ApplicationError):
    """Raised when validation fails."""
    pass

class NotFoundError(ApplicationError):
    """Raised when a resource is not found."""
    
    def __init__(self, resource: str, identifier: Any) -> None:
        self.resource = resource
        self.identifier = identifier
        super().__init__(f"{resource} not found: {identifier}")

class AuthorizationError(ApplicationError):
    """Raised when user is not authorized."""
    pass
```

### Result Type Pattern

```python
from typing import Generic, TypeVar, Union
from dataclasses import dataclass

T = TypeVar("T")
E = TypeVar("E", bound=Exception)

@dataclass
class Ok(Generic[T]):
    value: T

@dataclass
class Err(Generic[E]):
    error: E

Result = Union[Ok[T], Err[E]]

def divide(a: float, b: float) -> Result[float, ValueError]:
    if b == 0:
        return Err(ValueError("Division by zero"))
    return Ok(a / b)

# Usage
match divide(10, 2):
    case Ok(value):
        print(f"Result: {value}")
    case Err(error):
        print(f"Error: {error}")
```

## Performance Optimization

### Caching

```python
from functools import lru_cache
from datetime import datetime, timedelta

class TTLCache:
    def __init__(self, ttl_seconds: int) -> None:
        self.ttl = timedelta(seconds=ttl_seconds)
        self.cache: dict[str, tuple[Any, datetime]] = {}
    
    def get(self, key: str) -> Any | None:
        if key in self.cache:
            value, timestamp = self.cache[key]
            if datetime.now() - timestamp < self.ttl:
                return value
            del self.cache[key]
        return None
    
    def set(self, key: str, value: Any) -> None:
        self.cache[key] = (value, datetime.now())

# Simple LRU cache for pure functions
@lru_cache(maxsize=128)
def expensive_computation(n: int) -> int:
    # Compute something expensive
    ...
```

### Lazy Evaluation

```python
from typing import Iterator

def lazy_reader(filepath: str) -> Iterator[dict]:
    """Yield parsed records one at a time instead of loading all."""
    with open(filepath) as f:
        for line in f:
            yield json.loads(line)

# Process without loading entire file into memory
for record in lazy_reader("large_file.jsonl"):
    process(record)
```
