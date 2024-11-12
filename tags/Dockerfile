FROM python:3.12-slim

WORKDIR /tags
RUN pip install opensearch-py txtai
COPY ./tags/tag_adder.py ./tag_adder.py
CMD sleep 15 && python -u -m tag_adder