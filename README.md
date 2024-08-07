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
   - `/auto_unseal "True|False"`: Enable or disable the auto-unsealing feature.
   - `/fernet_key "keydata"`: Provide the Fernet key for encryption and decryption of unseal keys.
3. **Unseal Process**: Users provide their unseal keys through the bot. Once the required number of keys is collected, the bot attempts to unseal the Vault and verifies the unseal status.
4. **Rekey Process**: Users can initiate the rekey process, after which they provide their rekey keys. The bot collects these keys, completes the rekey process, and distributes the new keys to the users.
5. **Verification and Updates**: The bot continuously verifies the Vault's status and provides updates to users, ensuring transparency and security throughout the process.
6. **Timeout Mechanism**: The bot has a 10-minute window for users to provide the necessary keys for unseal and rekey operations. If the required keys are not provided within this window, the process times out and must be restarted.

## Fernet Key and Auto Unsealing

### Fernet Key

The bot requires a Fernet key to encrypt and decrypt unseal keys securely. The key must be a base64 encoded 32-byte key. This key is set once using the `/fernet_key "keydata"` command, and the bot will use it for all encryption and decryption operations.

To set the Fernet key:
```sh
/fernet_key "your_fernet_key_here"
```

### Auto Unsealing

Auto Unsealing allows the bot to automatically unseal the Vault when it detects that the Vault is sealed. When enabled, the bot will store the unseal keys securely and use them to unseal the Vault without user intervention.

To enable or disable Auto Unsealing:
```sh
/auto_unseal "True"
```
or
```sh
/auto_unseal "False"
```

When Auto Unsealing is enabled, the bot will:
1. Encrypt and store the provided unseal keys.
2. Attempt to unseal the Vault automatically if it detects that the Vault is sealed.
3. Broadcast a message to all authorized users once the Vault is successfully auto-unsealed.

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
   - `UNSEAL_KEYS_PATH`: Path to store the encrypted unseal keys (default is "./data").

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

