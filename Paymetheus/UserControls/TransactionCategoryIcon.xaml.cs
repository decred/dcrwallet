using Paymetheus.Decred.Wallet;
using System;
using System.Collections.Generic;
using System.Linq;
using System.Text;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Data;
using System.Windows.Documents;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using System.Windows.Navigation;
using System.Windows.Shapes;

namespace Paymetheus
{
    /// <summary>
    /// Interaction logic for TransactionStatusIcon.xaml
    /// </summary>
    public partial class TransactionCategoryIcon : Image
    {
        public TransactionCategoryIcon()
        {
            InitializeComponent();
        }

        public TransactionCategory Category
        {
            get { return (TransactionCategory)GetValue(CategoryProperty); }
            set { SetValue(CategoryProperty, value); }
        }

        public static DependencyProperty CategoryProperty = DependencyProperty.Register(nameof(Category), typeof(TransactionCategory),
            typeof(TransactionCategoryIcon), new PropertyMetadata(TransactionCategory.Unknown, OnCategoryChanged));

        private static void OnCategoryChanged(DependencyObject d, DependencyPropertyChangedEventArgs e)
        {
            var control = (TransactionCategoryIcon)d;
            var category = (TransactionCategory)e.NewValue;
            control.RenderCategory(category);
        }

        private void RenderCategory(TransactionCategory category)
        {
            switch (category)
            {
                case TransactionCategory.Send:
                    Source = new BitmapImage(new Uri("../Images/pm - icons - decrease.png", UriKind.Relative));
                    break;
                case TransactionCategory.Receive:
                    Source = new BitmapImage(new Uri("../Images/pm - icons - increase.png", UriKind.Relative));
                    break;
                default:
                    Source = null;
                    break;
            }
        }
    }
}
