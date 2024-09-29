# Use a base Python image
FROM python:3.9

# Set the working directory
WORKDIR /app

# Copy requirements.txt if you have one
COPY requirements.txt .

# Install dependencies
RUN pip install --no-cache-dir -r requirements.txt
RUN huggingface-cli login --token ******
# Copy the Python script into the container
COPY qps-test.py .

COPY ShareGPT_V3_unfiltered_cleaned_split.json .
