from pydantic import BaseModel, EmailStr


class AdminLogin(BaseModel):
    email: EmailStr
    password: str


class AccessToken(BaseModel):
    access_token: str
    token_type: str = "bearer"

