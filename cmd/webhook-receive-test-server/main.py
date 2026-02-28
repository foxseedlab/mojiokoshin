import json

import uvicorn
from fastapi import FastAPI, HTTPException, Request

app = FastAPI()


@app.post("/webhook")
async def receive_webhook(request: Request) -> dict[str, str]:
    try:
        payload = await request.json()
    except json.JSONDecodeError as exc:
        raise HTTPException(status_code=400, detail="Invalid JSON payload") from exc

    print(json.dumps(payload, indent=2, ensure_ascii=False), flush=True)
    return {"status": "ok"}


if __name__ == "__main__":
    uvicorn.run(app, host="0.0.0.0", port=8000)
