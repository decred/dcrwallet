// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Util;
using System;
using System.Collections.Generic;
using System.Linq;

namespace Paymetheus.Decred.Wallet
{
    public sealed class Wallet
    {
        /// <summary>
        /// The number of recent blocks which are always kept in memory in a synced wallet.
        /// Transactions from these recent blocks are used to subtract output amounts from
        /// the total balance to calculate spendable balances given some number of confirmations.
        /// This value may not be less than the coinbase maturity level, or immature coinbase
        /// outputs will not be subtracted from the spendable balance.
        /// </summary>
        public static int NumRecentBlocks(BlockChainIdentity blockChain) => blockChain.Maturity;

        /// <summary>
        /// The minimum number of transactions to always keep in memory for a synced wallet,
        /// so long as at least this many transactions are managed by the wallet process.
        /// If at least this number of transactions do not exist in the last NumRecentBlocks
        /// blocks and unmined transactions, earlier blocks and their transactions will be
        /// saved in memory.
        /// </summary>
        public const int MinRecentTransactions = 10;

        public const uint ImportedAccountNumber = 2147483647; // 2**31 - 1

        public Wallet(BlockChainIdentity activeChain, TransactionSet txSet, List<AccountProperties> bip0032Accounts,
            AccountProperties importedAccount, BlockIdentity chainTip)
        {
            if (activeChain == null)
                throw new ArgumentNullException(nameof(activeChain));
            if (bip0032Accounts == null)
                throw new ArgumentNullException(nameof(bip0032Accounts));
            if (importedAccount == null)
                throw new ArgumentNullException(nameof(importedAccount));
            if (chainTip == null)
                throw new ArgumentNullException(nameof(chainTip));

            _transactionCount = txSet.MinedTransactions.Aggregate(0, (acc, b) => acc + b.Transactions.Count) +
                txSet.UnminedTransactions.Count;
            _bip0032Accounts = bip0032Accounts;
            _importedAccount = importedAccount;

            var totalBalance = EnumerateAccounts().Aggregate((Amount)0, (acc, a) => acc + a.Item2.TotalBalance);

            ActiveChain = activeChain;
            RecentTransactions = txSet;
            TotalBalance = totalBalance;
            ChainTip = chainTip;
        }

        private int _transactionCount;
        private readonly List<AccountProperties> _bip0032Accounts;
        private readonly AccountProperties _importedAccount;

        public BlockChainIdentity ActiveChain { get; }
        public TransactionSet RecentTransactions { get; }
        public Amount TotalBalance { get; private set; }
        public BlockIdentity ChainTip { get; private set; }

        private void AddTransactionToTotals(WalletTransaction tx, Dictionary<Account, AccountProperties> modifiedAccounts)
        {
            var isCoinbase = BlockChain.IsCoinbase(tx.Transaction);

            foreach (var input in tx.Inputs)
            {
                TotalBalance -= input.Amount;

                var accountProperties = LookupAccountProperties(input.PreviousAccount);
                accountProperties.TotalBalance -= input.Amount;
                if (isCoinbase)
                    accountProperties.ImmatureCoinbaseReward -= input.Amount;
                modifiedAccounts[input.PreviousAccount] = accountProperties;
            }

            foreach (var output in tx.Outputs.OfType<WalletTransaction.Output.ControlledOutput>())
            {
                TotalBalance += output.Amount;

                var accountProperties = LookupAccountProperties(output.Account);
                accountProperties.TotalBalance += output.Amount;
                if (isCoinbase)
                    accountProperties.ImmatureCoinbaseReward += output.Amount;
                modifiedAccounts[output.Account] = accountProperties;
            }

            _transactionCount++;
        }

        private void RemoveTransactionFromTotals(WalletTransaction tx, Dictionary<Account, AccountProperties> modifiedAccounts)
        {
            var isCoinbase = BlockChain.IsCoinbase(tx.Transaction);

            foreach (var input in tx.Inputs)
            {
                TotalBalance += input.Amount;

                var accountProperties = LookupAccountProperties(input.PreviousAccount);
                accountProperties.TotalBalance += input.Amount;
                if (isCoinbase)
                    accountProperties.ImmatureCoinbaseReward += input.Amount;
                modifiedAccounts[input.PreviousAccount] = accountProperties;
            }

            foreach (var output in tx.Outputs.OfType<WalletTransaction.Output.ControlledOutput>())
            {
                TotalBalance -= output.Amount;

                var accountProperties = LookupAccountProperties(output.Account);
                accountProperties.TotalBalance -= output.Amount;
                if (isCoinbase)
                    accountProperties.ImmatureCoinbaseReward -= output.Amount;
                modifiedAccounts[output.Account] = accountProperties;
            }

            _transactionCount--;
        }

        public class ChangesProcessedEventArgs : EventArgs
        {
            // Update transactions, confirmations, balances.  Value is null if tip did not change.
            public BlockIdentity? NewChainTip { get; internal set; }

            // Transactions with a changed location (moved from unmined to a block,
            // block to another block, or block to unmined).
            public Dictionary<Blake256Hash, BlockIdentity> MovedTransactions { get; } = new Dictionary<Blake256Hash, BlockIdentity>();

            public List<Tuple<WalletTransaction, BlockIdentity>> AddedTransactions { get; } = new List<Tuple<WalletTransaction, BlockIdentity>>();

            public List<WalletTransaction> RemovedTransactions { get; } = new List<WalletTransaction>();

            public Dictionary<Account, AccountProperties> ModifiedAccountProperties { get; } = new Dictionary<Account, AccountProperties>();
        }

        public event EventHandler<ChangesProcessedEventArgs> ChangesProcessed;

        private void OnChangesProcessed(ChangesProcessedEventArgs e)
        {
            ChangesProcessed?.Invoke(this, e);
        }

        public void ApplyTransactionChanges(WalletChanges changes)
        {
            if (changes == null)
                throw new ArgumentNullException(nameof(changes));

            // A reorganize cannot be handled if the number of removed blocks exceeds the
            // minimum number saved in memory.
            if (changes.DetachedBlocks.Count >= NumRecentBlocks(ActiveChain))
                throw new BlockChainConsistencyException("Reorganize too deep");

            var newChainTip = changes.AttachedBlocks.LastOrDefault();
            if (ChainTip.Height >= newChainTip?.Height)
            {
                var msg = $"New chain tip {newChainTip.Hash} (height {newChainTip.Height}) neither extends nor replaces " +
                    $"the current chain (currently synced to hash {ChainTip.Hash}, height {ChainTip.Height})";
                throw new BlockChainConsistencyException(msg);
            }

            if (changes.NewUnminedTransactions.Any(tx => !changes.AllUnminedHashes.Contains(tx.Hash)))
                throw new BlockChainConsistencyException("New unmined transactions contains tx with hash not found in all unmined transaction hash set");

            var eventArgs = new ChangesProcessedEventArgs();

            var reorgedBlocks = RecentTransactions.MinedTransactions
                .ReverseList()
                .TakeWhile(b => changes.DetachedBlocks.Contains(b.Hash))
                .ToList();
            var numReorgedBlocks = reorgedBlocks.Count;
            foreach (var reorgedTx in reorgedBlocks.SelectMany(b => b.Transactions))
            {
                if (BlockChain.IsCoinbase(reorgedTx.Transaction) || !changes.AllUnminedHashes.Contains(reorgedTx.Hash))
                {
                    RemoveTransactionFromTotals(reorgedTx, eventArgs.ModifiedAccountProperties);
                }
                else
                {
                    RecentTransactions.UnminedTransactions[reorgedTx.Hash] = reorgedTx;
                    eventArgs.MovedTransactions.Add(reorgedTx.Hash, BlockIdentity.Unmined);
                }
            }
            var numRemoved = RecentTransactions.MinedTransactions.RemoveAll(block => changes.DetachedBlocks.Contains(block.Hash));
            if (numRemoved != numReorgedBlocks)
            {
                throw new BlockChainConsistencyException("Number of blocks removed exceeds those for which transactions were removed");
            }

            foreach (var block in changes.AttachedBlocks.Where(b => b.Transactions.Count > 0))
            {
                RecentTransactions.MinedTransactions.Add(block);

                foreach (var tx in block.Transactions)
                {
                    if (RecentTransactions.UnminedTransactions.ContainsKey(tx.Hash))
                    {
                        RecentTransactions.UnminedTransactions.Remove(tx.Hash);
                        eventArgs.MovedTransactions[tx.Hash] = block.Identity;
                    }
                    else if (!eventArgs.MovedTransactions.ContainsKey(tx.Hash))
                    {
                        AddTransactionToTotals(tx, eventArgs.ModifiedAccountProperties);
                        eventArgs.AddedTransactions.Add(Tuple.Create(tx, block.Identity));
                    }
                }
            }

            // TODO: What about new transactions which were not added in a newly processed
            // block (e.g. importing an address and rescanning for outputs)?

            foreach (var tx in changes.NewUnminedTransactions.Where(tx => !RecentTransactions.UnminedTransactions.ContainsKey(tx.Hash)))
            {
                RecentTransactions.UnminedTransactions[tx.Hash] = tx;
                AddTransactionToTotals(tx, eventArgs.ModifiedAccountProperties);

                // TODO: When reorgs are handled, this will need to check whether the transaction
                // being added to the unmined collection was previously in a block.
                eventArgs.AddedTransactions.Add(Tuple.Create(tx, BlockIdentity.Unmined));
            }

            var removedUnmined = RecentTransactions.UnminedTransactions
                .Where(kvp => !changes.AllUnminedHashes.Contains(kvp.Key))
                .ToList(); // Collect to list so UnminedTransactions can be modified below.
            foreach (var unmined in removedUnmined)
            {
                // Transactions that were mined rather than being removed from the unmined
                // set due to a conflict have already been removed.
                RecentTransactions.UnminedTransactions.Remove(unmined.Key);
                RemoveTransactionFromTotals(unmined.Value, eventArgs.ModifiedAccountProperties);
                eventArgs.RemovedTransactions.Add(unmined.Value);
            }

            if (newChainTip != null)
            {
                ChainTip = newChainTip.Identity;
                eventArgs.NewChainTip = newChainTip.Identity;
            }

            OnChangesProcessed(eventArgs);
        }

        public void UpdateAccountProperties(Account account, string name, uint externalKeyCount, uint internalKeyCount, uint importedKeyCount)
        {
            AccountProperties props;
            if (account.AccountNumber == ImportedAccountNumber)
            {
                props = _importedAccount;
            }
            else
            {
                var accountNumber = checked((int)account.AccountNumber);
                if (accountNumber < _bip0032Accounts.Count)
                {
                    props = _bip0032Accounts[accountNumber];
                }
                else if (accountNumber == _bip0032Accounts.Count)
                {
                    props = new AccountProperties();
                    _bip0032Accounts.Add(props);
                }
                else
                {
                    throw new Exception($"Account {accountNumber} is not the next BIP0032 account.");
                }
            }

            props.AccountName = name;
            props.ExternalKeyCount = externalKeyCount;
            props.InternalKeyCount = internalKeyCount;
            props.ImportedKeyCount = importedKeyCount;

            var eventArgs = new ChangesProcessedEventArgs();
            eventArgs.ModifiedAccountProperties[account] = props;
            OnChangesProcessed(eventArgs);
        }

        private static IEnumerable<WalletTransaction.Output.ControlledOutput> OutputsToAccount(WalletTransaction.Output[] outputs, Account account)
        {
            return outputs.OfType<WalletTransaction.Output.ControlledOutput>().Where(o => o.Account == account);
        }

        public Amount CalculateSpendableBalance(Account account, int minConf)
        {
            var balance = LookupAccountProperties(account).ZeroConfSpendableBalance;

            if (minConf == 0)
            {
                return balance;
            }

            var unminedTxs = RecentTransactions.UnminedTransactions;
            foreach (var output in unminedTxs.SelectMany(kvp => OutputsToAccount(kvp.Value.Outputs, account)))
            {
                balance -= output.Amount;
            }

            if (minConf == 1)
            {
                return balance;
            }

            var confHeight = BlockChain.ConfirmationHeight(ChainTip.Height, minConf);
            foreach (var block in RecentTransactions.MinedTransactions.ReverseList().TakeWhile(b => b.Height >= confHeight))
            {
                var unconfirmedTxs = block.Transactions;
                foreach (var output in unconfirmedTxs.SelectMany(tx => OutputsToAccount(tx.Outputs, account)))
                {
                    balance -= output.Amount;
                }
            }

            return balance;
        }

        // TODO: This only supports BIP0032 accounts currently (NOT the imported account).
        // Results are indexed by their account number.
        // TODO: set locked balances
        public Balances[] CalculateBalances(int requiredConfirmations)
        {
            // Initial balances.  Unspendable outputs are subtracted from the totals.
            // TODO: set locked balance
            var balances = _bip0032Accounts.Select(a => new Balances(a.TotalBalance, a.TotalBalance, 0)).ToArray();

            if (requiredConfirmations == 0)
            {
                return balances;
            }

            var controlledUnminedOutputs = RecentTransactions.UnminedTransactions
                .SelectMany(kvp => kvp.Value.Outputs)
                .OfType<WalletTransaction.Output.ControlledOutput>()
                .Where(a => a.Account.AccountNumber != ImportedAccountNumber); // Imported is not reported currently.
            foreach (var unminedOutput in controlledUnminedOutputs)
            {
                var accountNumber = unminedOutput.Account.AccountNumber;
                balances[checked((int)accountNumber)].SpendableBalance -= unminedOutput.Amount;
            }

            if (requiredConfirmations == 1)
            {
                return balances;
            }

            var confHeight = BlockChain.ConfirmationHeight(ChainTip.Height, requiredConfirmations);
            foreach (var block in RecentTransactions.MinedTransactions.ReverseList().TakeWhile(b => b.Height >= confHeight))
            {
                var controlledMinedOutputs = block.Transactions
                    .SelectMany(tx => tx.Outputs)
                    .OfType<WalletTransaction.Output.ControlledOutput>()
                    .Where(a => a.Account.AccountNumber != ImportedAccountNumber); // Imported is not reported currently.
                foreach (var output in controlledMinedOutputs)
                {
                    var accountNumber = output.Account.AccountNumber;
                    balances[checked((int)accountNumber)].SpendableBalance -= output.Amount;
                }
            }

            return balances;
        }

        public string OutputDestination(WalletTransaction.Output output)
        {
            if (output == null)
                throw new ArgumentNullException(nameof(output));

            if (output is WalletTransaction.Output.ControlledOutput)
            {
                var controlledOutput = (WalletTransaction.Output.ControlledOutput)output;
                if (controlledOutput.Change)
                    return "Change";
                else
                    return LookupAccountProperties(controlledOutput.Account).AccountName;
            }
            else
            {
                var uncontrolledOutput = (WalletTransaction.Output.UncontrolledOutput)output;
                Address address;
                if (Address.TryFromOutputScript(uncontrolledOutput.PkScript, ActiveChain, out address))
                    return address.Encode();
                else
                    return "Non-address output";
            }
        }

        public AccountProperties LookupAccountProperties(Account account)
        {
            if (account.AccountNumber == ImportedAccountNumber)
            {
                return _importedAccount;
            }

            var accountIndex = checked((int)account.AccountNumber);
            return _bip0032Accounts[accountIndex];
        }

        public string AccountName(Account account) => LookupAccountProperties(account).AccountName;

        public IEnumerable<TupleValue<Account, AccountProperties>> EnumerateAccounts()
        {
            return _bip0032Accounts.Select((p, i) => TupleValue.Create(new Account((uint)i), p))
                .Concat(new[] { TupleValue.Create(new Account(ImportedAccountNumber), _importedAccount) });
        }
    }
}
