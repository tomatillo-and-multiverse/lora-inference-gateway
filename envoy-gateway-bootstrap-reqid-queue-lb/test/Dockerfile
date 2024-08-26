# Use a base Python image
FROM python:3.9

# Set the working directory
WORKDIR /app

# Copy requirements.txt if you have one
COPY requirements.txt .

# Install dependencies
RUN pip install --no-cache-dir -r requirements.txt

# Copy the Python script into the container
COPY qps1.py .

# Set the default command to run your script
CMD ["python", "qps1.py"]
