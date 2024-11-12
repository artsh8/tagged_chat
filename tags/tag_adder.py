import os
import time
from opensearchpy import OpenSearch

# обход ошибки: Error #15: Initializing libomp140.x86_64.dll, but found libiomp5md.dll already initialized
os.environ["KMP_DUPLICATE_LIB_OK"] = "TRUE"
from txtai import Embeddings


host = os.getenv("HOST")
port = os.getenv("PORT")
auth = (os.getenv("USERNAME"), os.getenv("PASSWORD"))
index_name = "chat"
client = OpenSearch(
    hosts=[{"host": host, "port": port}],
    http_auth=auth,
    use_ssl=True,
    verify_certs=False,
    ssl_show_warn=False,
)
embeddings = Embeddings(path="snagbreac/russian-reverse-dictionary-semsearch", content=False)
tags = []


def msgs_to_tag(ts: int) -> tuple[int, list[dict]]:
    query = {
        "query": {
            "bool": {
                "must": [{"range": {"ts": {"lte": ts}}}, {"term": {"tag.keyword": ""}}],
            }
        },
        "size": 10,
        "sort": [{"ts": {"order": "desc"}}],
    }
    try:
        res = client.search(body=query, index=index_name)
    except:
        print("Ошибка чтения из Opensearch")
        total = 0
        hits = []
    else:
        total = res["hits"]["total"]["value"]
        hits = res["hits"]["hits"]
    return total, hits


def tag_for(msg: str) -> str:
    res = embeddings.search(msg, 1)
    if res[0][1] > 0.3:
        return tags[res[0][0]]
    else:
        return ""


def add_tags() -> None:
    _, hits = msgs_to_tag(time.time())
    if hits:
        for hit in hits:
            content = hit["_source"]["content"]
            tag = tag_for(content)
            if tag:
                doc_id = hit["_id"]
                try:
                    res = client.update(
                        index=index_name, id=doc_id, body={"doc": {"tag": tag}}
                    )
                    print(res)
                except:
                    print("Ошибка обновления тэга в OpenSearch")


def get_new_tags() -> list:
    try:
        res = client.get(index="tags", id=1)
    except:
        print("Ошибка чтения из Opensearch")
        new_tags = tags
    else:
        new_tags = res["_source"]["tags"]    
    return new_tags


def update_tag_list() -> None:
    global tags
    new_tags = get_new_tags()
    if tags != new_tags:
        tags = new_tags
        embeddings.index(tags)
        print("Обновлены лейблы для тэгов")


def main() -> None:
    throttle = int(os.getenv("THROTTLE"))
    while True:
        print("Проставление тэгов")
        update_tag_list()
        add_tags()
        time.sleep(throttle)


if __name__ == "__main__":
    main()
