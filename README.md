# Vault Engineer Bot

## Problem Statement

Managing and unsealing a HashiCorp Vault can be a complex task, especially when coordinating the process among multiple users who possess different unseal keys. Additionally, the rekey process adds another layer of complexity requiring careful coordination and security.

## Purpose

The Vault Engineer Bot is designed to simplify and automate the process of managing HashiCorp Vault's unseal and rekey operations. It allows authorized users to easily provide their unseal keys, initiate rekey processes, and receive new keys, all through a convenient Telegram bot interface.

## Use Case

- **Automated Vault Unseal**: Coordinate unseal key submissions from multiple users and ensure the Vault is correctly unsealed.
- **Rekey Management**: Initiate and manage the rekey process, ensuring that new keys are distributed securely and efficiently among authorized users.
- **Status Updates**: Provide real-time updates on the Vault's status and notify users of any issues during the unseal or rekey processes.

## How It Works

1. **Initialization**: The bot is initialized with environment variables specifying the Vault's required unseal keys, total keys, and authorized Telegram users.
2. **Commands**:
   - `/start`: Welcome message to the bot.
   - `/vault_status`: Get the current status of the Vault.
   - `/unseal "key"`: Provide an unseal key. The bot collects the required number of keys and attempts to unseal the Vault.
   - `/rekey_init`: Initiate the rekey process, enabling the `/rekey_init_keys` command.
   - `/rekey_init_keys "key"`: Provide a rekey key during the rekey process.
   - `/rekey_cancel`: Cancel the ongoing rekey process.
   - `/refresh`: Reset the bot state, discarding ongoing unseal or rekey operations.
   - `/help`: Display available commands.
3. **Unseal Process**: Users provide their unseal keys through the bot. Once the required number of keys is collected, the bot attempts to unseal the Vault and verifies the unseal status.
4. **Rekey Process**: Users can initiate the rekey process, after which they provide their rekey keys. The bot collects these keys, completes the rekey process, and distributes the new keys to the users.
5. **Verification and Updates**: The bot continuously verifies the Vault's status and provides updates to users, ensuring transparency and security throughout the process.
6. **Timeout Mechanism**: The bot has a 10-minute window for users to provide the necessary keys for unseal and rekey operations. If the required keys are not provided within this window, the process times out and must be restarted.

## How to Get User IDs from Telegram

- To authorize users for the bot, you need their Telegram user IDs. Follow these steps to obtain them:
  - Use the Bot "My User ID":
  - In Telegram, search for the bot with the username @UserIDxBot.
  - Start a chat with the bot.
  - Use the command /id to fetch your Telegram user ID. The bot will reply with your user ID.

## Deployment

1. **Environment Variables**: Ensure the following environment variables are set:

   - `TELEGRAM_BOT_TOKEN`: The token provided by BotFather for your Telegram bot.
   - `VAULT_REQUIRED_KEYS`: The number of keys required to unseal the Vault.
   - `VAULT_TOTAL_KEYS`: The total number of keys.
   - `TELEGRAM_USERS`: Comma-separated list of authorized Telegram UserIds.
   - `VAULT_HOST`: The URL of your Vault instance.

2. **Build and Run the Bot locally**: To run the bot locally. Ensure all dependencies are installed and the environment variables are correctly set.

```sh
export LOCAL=true
make build
make run
```

3. **Interaction**: Authorized users can interact with the bot via Telegram to manage the Vault's unseal and rekey processes.

## Edge Cases Considered

- **Duplicate Keys from the Same User**: The bot ensures that a user can only provide one key per process. If a user tries to provide multiple keys, the bot discards the extra keys and asks for keys from other users.
- **Same Key from Different Users**: If the same key is provided by different users, the bot will broadcast a message indicating a violation.
- **Vault Already Unsealed**: The bot checks the Vault's status before accepting unseal keys to ensure it doesn't collect keys unnecessarily.
- **Ongoing Rekey Process**: The bot checks if a rekey process is already in progress before initiating a new one, ensuring proper handling of concurrent operations.
- **Timeout Handling**: If the required keys are not provided within 10 minutes, the bot resets the state and cancels the operation.
- **Broadcast Messages**: The bot broadcasts the success or failure of the unseal or rekey operations to all authorized users, ensuring everyone is informed of the current status.

## Development and Future Enhancements

- **Adding New Commands**: Follow the structure of existing commands to add new functionalities.
- **Improving Security**: Consider implementing more robust security measures such as encrypted communication between the bot and the Vault server.
- **Enhancing User Experience**: Add more detailed status messages and user feedback to improve interaction with the bot.
- **Scalability**: Ensure the bot can handle a larger number of users and keys as your Vault environment grows.

## Contributing

We welcome contributions from the community. Please submit your pull requests with detailed descriptions of the changes and the problem they solve. Make sure to run tests and follow the code style of the project.

## Code of Conduct

Instances of abusive, harassing, or otherwise unacceptable behavior may be reported to the community leaders responsible for enforcement at [gnana097@gmail.com, dharamendra.kariya@gmail.com] . All complaints will be reviewed and investigated promptly and fairly.

All community leaders are obligated to respect the privacy and security of the reporter of any incident.

---

Feel free to customize this template further based on your specific requirements and deployment details.

---

## Next Plan:

- Ask for a Fernt key from user when the bot starts! Without this bot will not get initialized ( no commands other than /fernet_key will be working) - DONE

- If it doesnt get fernet key then send the brodcast alert to all the users that I can not operate , please send the fernet! - DONE

- Fernet key should be given like /fernet_key "" - DONE

- Fernet key should be given like /fernet_key "" ( only once by any one user ) if its there and someone else also give that key or any other key just say that fernte key has already been given by user_name - DONE

- Now using this fernet key, store all the encrypted unseal keys using the given fernet key - DONE

- Add handler /auto_unseal "True" which will enable the auto unsealing  - "False" will disable it - DONE


- Accept vault urls and name in key value pair , this variable VAULT_HOST="" should be discarded and replace it with the json file named vault_hosts.json

   which should have key value pair like 

   name: url
   vault1: url1
   vault2: url2
   vault3: url3

- Bot sohuld check the vault status for all these given vaults ( configure in json )

- If any vault is down then send a broadcast msg to all users that the "vault_name with the url" is down

- And at the same time auto unseal it using the stored encrypted unseal keys ! obviously you should decrypte the key first! - DONE

- Nice to have: /vault_init vault_name 

   ( this handler will initialize the vault with the default total_key_share and require_key_share ) 

   But before this check if the vault is initialized or not? then only proceed
   
   This will initialize the vault and store the encrypted unseal keys on disk ( at this location /data/unsealkeys/vaut_name )

   This handler will return error if the given vault name is not found in configured json


- Also all the vault data ( encrypted unseal keys will stored at /data/unsealkeys/vault_name ) - DONE

- /unseal command will be used as /unseal vault_name "" ( the vault name will be checked in configured json)

- /rekey_init_keys will be used as /rekey_init_keys vault_name ""

- Newely generated unseal keys by rekey process should update the encrpted data on disk (here /data/unsealkeys/vault_name) - DONE

   And at the same should send the original each key to respective user ( which we already have )  - DONE

- /vault_status will be used as /vault_status vault_name 

- Nice to have : If possible improve menu card 

- Nice to have : If possible give /dashboard which shows all the vaults name in green if unsealed , in red if sealed 

### Fernte Example:

- /fernet_key "-YcQiZeifWj8_p0PfYz6Y9CAnKAP4PSuoRqxeTqO0uY="

- /fernet_key "i6Kthxe-2YP40B0VpMNoXZ7jFaEC3ZT0ygCXHPjndyw="

- /fernet_key "-QzY-F9MBicQ7wJ0tY1K0MlJYAfZm8OxWcYoC1o8s5I="

its generated by this

```
from cryptography.fernet import Fernet

# Generate a key
key = Fernet.generate_key()
print(key.decode())  # Display the key
```
